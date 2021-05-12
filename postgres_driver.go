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

	p := &PostgresDriver{
		tableName:  dbTable,
		schemaName: dbSchema,
	}

	p.db, err = sql.Open("postgres", connString)

	if err != nil {
		return nil, err
	}

	return p, err
}

func (p *PostgresDriver) taskQueryColumns() string {
	return "a." + p.primaryKey() + ", a.task_key, a.task_name, a.created_at, a.created_by, a.data, a.state"
}

func (p *PostgresDriver) primaryKey() string {
	return p.tableName + "_id"
}

// clear Removes all entries from the queue.  Be careful.  Generally you should cancel entries rather than delete.
func (p *PostgresDriver) clear() error {
	_, err := p.db.Exec(fmt.Sprintf("DELETE FROM %s", p.schemaTable()))

	return err
}

func (p *PostgresDriver) name() string {
	return "PostgresDriver"
}

// AddTask Adds a task to the queue
func (p *PostgresDriver) addTask(taskData TaskInit) error {
	// Store data as json:
	dataString, err := json.Marshal(taskData.Data)

	created := time.Now()
	// Convert
	_, err = p.db.Exec(`
SET search_path = `+p.schemaName+`;
INSERT INTO `+p.schemaTable()+`
	(`+p.primaryKey()+`, data, state, task_key, task_name, created_at, last_attempted, last_attempt_message, do_after, created_by)
VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, 'Created', $7, $8)`,
		dataString,
		"READY",
		taskData.Key,
		taskData.Name,
		created,
		created,
		taskData.DoAfter,
		taskData.CreatedBy,
	)

	return err
}

func (p *PostgresDriver) cleanup(task Task) {
	if task.tx != nil {
		// Possibly already committed/rolled back by this stage.  E.g., if unmarshal in pop() has called commit
		task.tx.Commit()
	}
}

func (p *PostgresDriver) pop() (Task, error) {
	var task Task
	var data string

	tx, err := p.db.Begin()
	if err != nil {
		return task, err
	}

	query := `
WITH u AS (
	SELECT ` + p.primaryKey() + `
	FROM ` + p.schemaTable() + `
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
UPDATE ` + p.schemaTable() + ` a SET last_attempted=Now(), last_attempt_message='Attempting', state='` + string(TaskRetry) + `'
FROM u
WHERE a.` + p.primaryKey() + ` = u.` + p.primaryKey() + `
RETURNING ` + p.taskQueryColumns()

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

func (p *PostgresDriver) refreshRetry(age time.Duration) error {
	when := time.Now().Add(-age)
	_, err := p.db.Exec("UPDATE "+p.schemaTable()+" SET state=$1, last_attempted=$2 WHERE state=$3 AND last_attempted < $4", string(TaskReady), time.Now(), string(TaskRetry), when)

	return err
}

func (p *PostgresDriver) getQueueLength() (int64, error) {
	var length int64

	err := p.db.QueryRow("SELECT count(*) FROM " + p.schemaTable() + " LIMIT 1").Scan(&length)

	return length, err
}

func (p *PostgresDriver) getTaskCount(taskName string) (int64, error) {
	var length int64

	err := p.db.QueryRow("SELECT count(*) FROM "+p.schemaTable()+" WHERE task_name = $1 AND state != 'CANCELLED' AND state != 'DONE'", taskName).Scan(&length)

	return length, err
}

func (p *PostgresDriver) complete(task Task, message string) error {
	return p.setTaskState(task, TaskDone, message)
}

func (p *PostgresDriver) cancel(task Task, message string) error {
	return p.setTaskState(task, TaskCancelled, message)
}

func (p *PostgresDriver) fail(task Task, message string) error {
	return p.setTaskState(task, TaskFailed, message)
}

func (p *PostgresDriver) retry(task Task, message string) error {
	return p.setTaskState(task, TaskRetry, message)
}

func (p *PostgresDriver) setTaskState(task Task, state TaskState, message string) error {
	if task.tx == nil {
		return fmt.Errorf("Cannot have nil transaction for task")
	}
	_, err := task.tx.Exec("UPDATE "+p.schemaTable()+" SET state=$1, last_attempted=$2, last_attempt_message=$3 WHERE "+p.primaryKey()+" = $4", string(state), time.Now(), message, task.id)

	if err != nil {
		task.tx.Rollback()
		return err
	}
	return task.tx.Commit()
}

func (p *PostgresDriver) scanTask(scanner *sql.Row) (Task, error) {
	var task Task
	var data string

	err := scanner.Scan(&task.id, &task.Key, &task.Name, &task.Created, &task.CreatedBy, &data, &task.State)

	if err != nil {
		return task, err
	}

	err = json.Unmarshal([]byte(data), &task.Data)

	return task, err
}
