package queue

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// NewSyncManager Returns a new and ready sync manager
func NewSyncManager(driver Driver) SyncManager {
	var sm SyncManager
	sm.driver = driver
	sm.registeredActions = make(map[string]TaskAction)
	sm.actionStreams = make(map[string]chan ScheduledAction)
	sm.taskQueue = make(chan taskQueueAction)
	sm.cancel = make(chan bool)
	sm.registerMutex = &sync.Mutex{}

	sm.errorHandler = defaultErrorHandler
	mx := sync.Mutex{}
	sm.getStreamMX = &mx

	return sm
}

type taskQueueAction struct {
	Task Task
	Done chan bool
}

// SyncManager is the central process for running actions
type SyncManager struct {
	actionStreams     map[string]chan ScheduledAction
	taskQueue         chan taskQueueAction
	cancel            chan bool
	driver            Driver
	registeredActions map[string]TaskAction
	registerMutex     *sync.Mutex
	errorHandler      func(error)
	getStreamMX       *sync.Mutex
}

func (s *SyncManager) getStreamQueue(name string) chan ScheduledAction {
	var stream chan ScheduledAction
	var ok bool

	s.getStreamMX.Lock()
	defer s.getStreamMX.Unlock()

	if stream, ok = s.actionStreams[name]; !ok {
		// No such stream exists, so let's create first
		stream = make(chan ScheduledAction)
		s.actionStreams[name] = stream
		// Run a goroutine that handles actions from this stream:
		s.runStream(stream)
	}

	return stream
}

// Run Runs the main loop that keeps the queue running and performs actions at specified intervals
func (s *SyncManager) Run() {

	cancelQueue := make(chan bool)
	// Start the synchroniser queue handler:
	go s.runQueue(cancelQueue)

	for {
		select {
		case <-s.cancel:
			cancelQueue <- true
			return
		case tqa := <-s.taskQueue:
			var err error
			task := tqa.Task
			action := s.getRegisteredAction(task.Name)

			if action == nil {
				err = fmt.Errorf("cancelling task with ID %s because there is no action to handle it", task.id)
				s.errorHandler(err)
				err = s.driver.cancel(task, err.Error())
				if err != nil {
					s.errorHandler(err)
				}
			} else {
				result, message := action.Do(task)
				switch result {
				case TaskResultPermanentFailure, TaskResultRetryFailure:
					// Task failed
					s.errorHandler(fmt.Errorf("%s", message))

					switch result {
					case TaskResultPermanentFailure:
						err = s.driver.fail(task, message)
					case TaskResultRetryFailure:
						err = s.driver.retry(task, message)
					default:
						err = fmt.Errorf("Undefined task result %s", result)
					}

					if err != nil {
						s.errorHandler(err)
					}
				case TaskResultSuccess:
					// Complete the task
					err = s.driver.complete(task, message)
					if err != nil {
						s.errorHandler(err)
					}
				default:
					s.errorHandler(fmt.Errorf("fell through: undefined task result %s", result))
				}
			}

			s.driver.cleanup(task)
			tqa.Done <- true
		}
	}
}

// runStream By separating tasks into separate streams, we can have some
// scheduled actions run side by side, and others that run separately.  For
// example, Netsuite doesn't like multiple connections, so all such scheduled
// actions may go into one stream.  On the other hand, actions that run against
// a Postgres database may be able to run simultaneously.  runStream receives
// actions on its stream, and blocks on that stream until the action is
// complete.
func (s *SyncManager) runStream(stream chan ScheduledAction) {
	n := time.Now()
	go func() {
		fmt.Printf("Starting a new stream at %s\n", n)
		for {
			select {
			case action := <-stream:
				err := action.Do()
				if err != nil {
					s.errorHandler(err)
				}
			}
		}
	}()
}

func (s *SyncManager) runQueue(cancel chan bool) {

	refreshDelay := time.Second * 4 // refreshDelay defines how soon before refreshing tasks that need to be retried
	refreshed := time.Now()

	for {

		select {
		case <-cancel:
			return

		default:
			// Refresh tasks marked for retry:
			if time.Now().Sub(refreshed) >= refreshDelay {
				err := s.driver.refreshRetry(time.Hour)

				if err != nil {
					s.errorHandler(err)
				}

				refreshed = time.Now()
			}

			// Check for new tasks in queue:
			task, err := s.driver.pop()

			if err != nil && err != ErrNoTasks {
				s.driver.cleanup(task)
				s.errorHandler(err)
			} else if err != ErrNoTasks {
				// We want to wait until this is executed before we begin the task again.
				// Otherwise "pop" might return the same value, since it's not truly pop'ing

				reply := make(chan bool)
				s.taskQueue <- taskQueueAction{Task: task, Done: reply}
				<-reply
			}

			time.Sleep(1 * time.Second)
		}
	}
}

// Stop Stops the sync manager main loop
func (s *SyncManager) Stop() {
	s.cancel <- true
}

// Schedule Schedule an action to be performed at particular intervals
func (s *SyncManager) Schedule(act ScheduledAction, period time.Duration) {
	ticker := time.NewTicker(period)

	// We fetch a reference to the stream's channel so that we can schedule
	// our task
	stream := s.getStreamQueue(act.Stream())

	go func(act ScheduledAction, ticker *time.Ticker) {
		for {
			<-ticker.C

			stream <- act
		}
	}(act, ticker)
}

// RegisterTaskHandler Specifies which action to be used to handle a task of name taskName
func (s *SyncManager) RegisterTaskHandler(act TaskAction, taskName string) error {
	s.registerMutex.Lock()
	s.registeredActions[taskName] = act
	s.registerMutex.Unlock()

	return nil
}

func (s *SyncManager) getRegisteredAction(taskName string) TaskAction {
	var taskAction TaskAction

	s.registerMutex.Lock()
	taskAction = s.registeredActions[taskName]
	s.registerMutex.Unlock()

	return taskAction
}

// SetErrorHandler Sets a function to handle errors from the run function
func (s *SyncManager) SetErrorHandler(handler func(err error)) {
	s.errorHandler = handler
}

func defaultErrorHandler(err error) {
	log.Print(err)
}
