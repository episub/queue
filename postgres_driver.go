package queue

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/lib/pq"
)

// PostgresDriver PostgreSQL Driver
type PostgresDriver struct {
	tableName  string
	schemaName string
	db         *sql.DB
}

// schemaTable returns appropriate table+schema name
func (p PostgresDriver) schemaTable() string {
	if len(p.schemaName) > 0 {
		return p.schemaName + "." + p.tableName
	}

	return p.tableName
}

// NewPostgresDriver Returns a new postgres driver, initialised.  readTimeout is in seconds
func NewPostgresDriver(connString string, dbSchema string, dbTable string) (*PostgresDriver, error) {
	var err error

	d := &PostgresDriver{
		tableName:  dbTable,
		schemaName: dbSchema,
	}

	d.db, err = sql.Open("postgres", connString)

	if err != nil {
		return nil, err
	}

	return d, err
}

func (d *PostgresDriver) taskQueryColumns() string {
	return "a." + d.primaryKey() + ", a.task_key, a.task_name, a.created_at, a.created_by, a.data, a.state"
}

func (d *PostgresDriver) primaryKey() string {
	return d.tableName + "_id"
}

// clear Removes all entries from the queue.  Be careful.  Generally you should cancel entries rather than delete.
func (d *PostgresDriver) clear() error {
	_, err := d.db.Exec(fmt.Sprintf("DELETE FROM %s", d.schemaTable()))

	return err
}

func (d *PostgresDriver) name() string {
	return "PostgresDriver"
}

// AddTask Adds a task to the queue
func (d *PostgresDriver) addTask(taskName string, taskKey string, doAfter time.Time, data map[string]interface{}) error {
	// Store data as json:
	dataString, err := json.Marshal(data)

	created := time.Now()
	// Convert
	_, err = d.db.Exec(`
INSERT INTO `+d.schemaTable()+`
	(`+d.primaryKey()+`, data, state, task_key, task_name, created_at, last_attempted, last_attempt_message, do_after)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, 'Created', $7)`,
		dataString,
		"READY",
		taskKey,
		taskName,
		created,
		created,
		doAfter,
	)

	return err
}

// func (d *PostgresDriver) getTask(taskName string) (Task, error) {
// 	var task Task
// 	var err error
//
// 	query := `SELECT ` + d.taskQueryColumns() + ` FROM ` + d.schemaTable() + ` WHERE task_name = $1 ORDER BY created_at DESC LIMIT 1`
//
// 	task, err = d.scanTask(d.db.QueryRow(query))
//
// 	return task, err
// }

func (d *PostgresDriver) cleanup(task Task) {
	if task.tx != nil {
		// Possibly already committed/rolled back by this stage.  E.g., if unmarshal in pop() has called commit
		task.tx.Commit()
	}
}

func (d *PostgresDriver) pop() (Task, error) {
	var task Task
	var data string

	tx, err := d.db.Begin()
	if err != nil {
		return task, err
	}

	query := `
WITH u AS (
	SELECT ` + d.primaryKey() + `
	FROM ` + d.schemaTable() + `
	WHERE (
		state IN ('` + string(TaskReady) + `')
		OR (
			last_attempted < Now() - INTERVAL '10 minute'
			AND state IN ('` + string(TaskInProgress) + `', '` + string(TaskRetry) + `')
		)
	)
	AND do_after < Now()
	ORDER BY last_attempted ASC
	FOR UPDATE SKIP LOCKED
	LIMIT 1
)
UPDATE ` + d.schemaTable() + ` a SET last_attempted=Now(), last_attempt_message='Attempting', state='` + string(TaskRetry) + `'
FROM u
WHERE a.` + d.primaryKey() + ` = u.` + d.primaryKey() + `
RETURNING ` + d.taskQueryColumns()

	err = tx.QueryRow(query).Scan(&task.id, &task.Key, &task.Name, &task.Created, &task.CreatedBy, &data, &task.State)
	task.tx = tx

	if err == sql.ErrNoRows {
		tx.Rollback()
		return task, ErrNoTasks
	}

	if err != nil {
		// Error calling query to get next off the ramp
		tx.Rollback()
		return task, err
	}

	err = json.Unmarshal([]byte(data), &task.Data)

	if err != nil {
		// Defaults to retry, as per query above:
		tx.Commit()
	}

	return task, err
}

func (d *PostgresDriver) refreshRetry(age time.Duration) error {
	when := time.Now().Add(-age)
	_, err := d.db.Exec("UPDATE "+d.schemaTable()+" SET state=$1, last_attempted=$2 WHERE state=$3 AND last_attempted < $4", string(TaskReady), time.Now(), string(TaskRetry), when)

	return err
}

func (d *PostgresDriver) getQueueLength() (int64, error) {
	var length int64

	err := d.db.QueryRow("SELECT count(*) FROM " + d.schemaTable() + " LIMIT 1").Scan(&length)

	return length, err
}

func (d *PostgresDriver) complete(task Task, message string) error {
	return d.setTaskState(task, TaskDone, message)
}

func (d *PostgresDriver) cancel(task Task, message string) error {
	return d.setTaskState(task, TaskCancelled, message)
}

func (d *PostgresDriver) fail(task Task, message string) error {
	return d.setTaskState(task, TaskFailed, message)
}

func (d *PostgresDriver) retry(task Task, message string) error {
	return d.setTaskState(task, TaskRetry, message)
}

func (d *PostgresDriver) setTaskState(task Task, state TaskState, message string) error {
	if task.tx == nil {
		return fmt.Errorf("Cannot have nil transaction for task")
	}
	_, err := task.tx.Exec("UPDATE "+d.schemaTable()+" SET state=$1, last_attempted=$2, last_attempt_message=$3 WHERE "+d.primaryKey()+" = $4", string(state), time.Now(), message, task.id)

	if err != nil {
		task.tx.Rollback()
		return err
	}
	return task.tx.Commit()
}

func (d *PostgresDriver) scanTask(scanner *sql.Row) (Task, error) {
	var task Task
	var data string

	err := scanner.Scan(&task.id, &task.Key, &task.Name, &task.Created, &task.CreatedBy, &data, &task.State)

	if err != nil {
		return task, err
	}

	err = json.Unmarshal([]byte(data), &task.Data)

	return task, err
}
