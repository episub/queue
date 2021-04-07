package queue

import (
	"errors"
	"time"
)

// Driver Manages the connection to the background queue to keep track of tasks
type Driver interface {
	clear() error // Clears the queue.  Obviously, be careful
	addTask(taskName string, taskKey string, doAfter time.Time, data map[string]interface{}) error
	// getTask(taskName string) (Task, error) // Grabs most recent entry for that task name
	name() string // Returns a name for the driver

	// pop Grabs the earliest task that's ready for action
	pop() (Task, error)

	// cleanup Gives the driver a chance to clean up the task, such as closing
	// off any transactions
	cleanup(Task)

	// refreshRetry Refreshes all tasks marked as retry that are older than the specified interval
	refreshRetry(age time.Duration) error
	// complete Marks a task as complete
	complete(task Task, message string) error
	// cancel Marks a task as cancelled
	cancel(task Task, message string) error
	// fail Marks a task as permanently failed
	fail(task Task, message string) error
	// retry Marks a task as temporarily failed and in need of a retry later
	retry(task Task, message string) error

	getQueueLength() (int64, error)
}

// ErrNoTasks Returned when there are no tasks available in the queue
var ErrNoTasks = errors.New("No tasks available")
