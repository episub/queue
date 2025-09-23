package queue

func NewExampleScheduledAction(result chan bool, panicCount int) ExampleScheduledAction {
	ea := ExampleScheduledAction{panicCount: panicCount}

	ea.result = result

	return ea
}

type ExampleScheduledAction struct {
	result     chan bool
	panicCount int
}

func (ea *ExampleScheduledAction) Do() error {
	if ea.panicCount > 0 {
		ea.panicCount--
		panic("failing here")
	}
	ea.result <- true
	return nil

}

func (ea *ExampleScheduledAction) Stream() string {
	return "test"

}

func NewExampleTaskAction(result chan bool) ExampleTaskAction {
	ea := ExampleTaskAction{}

	ea.result = result

	return ea
}

type ExampleTaskAction struct {
	result chan bool
}

func (ea *ExampleTaskAction) Do(task Task) (TaskResult, string) {
	ea.result <- true
	return TaskResultSuccess, "Done"
}
