package compensate

// // secClientMaxQueueMessages is the maximum number of messages that can be queued from the client.
// const secClientMaxQueueMessages = 2

// // secExecMaxQueueMessages is the maximum number of messages that can be queued from SagaExecutors.
// const secExecMaxQueueMessages = 2

// // SecClient is the client handle for a Saga Execution Coordinator (SEC).
// type SecClient struct {
// 	cmdTx      chan secClientMsg
// 	task       *Task
// 	shutdown   bool
// 	shutdownWg *sync.WaitGroup
// }

// // SagaCreate creates a new saga.
// func (c *SecClient) SagaCreate(ctx context.Context, sagaID SagaID, uctx interface{}, dag *SagaDag, registry *ActionRegistry) (*Task, error) {
// 	ackTx := make(chan *Task, 1) // Channel to receive the task future
// 	// TODO: Convert TemplateParamsForCreate
// 	templateParams := &TemplateParamsForCreate{}
// 	c.cmdTx <- secClientMsg{
// 		Type:           sagaCreate,
// 		SagaID:         sagaID,
// 		TemplateParams: templateParams,
// 		Dag:            dag,
// 		Ack:            ackTx,
// 	}
// 	select {
// 	case task := <-ackTx:
// 		return task, nil
// 	case <-ctx.Done():
// 		return nil, ctx.Err()
// 	}
// }

// // SagaResume resumes a saga that was previously running.
// func (c *SecClient) SagaResume(ctx context.Context, sagaID SagaID, uctx interface{}, dagJSON []byte, registry *ActionRegistry, logEvents []*SagaNodeEvent) (*Task, error) {
// 	// ... (Implementation to resume a saga)
// 	return nil, fmt.Errorf("SagaResume not implemented")
// }

// // SagaStart starts running (or resumes running) a saga.
// func (c *SecClient) SagaStart(ctx context.Context, sagaID SagaID) error {
// 	ackTx := make(chan error, 1)
// 	c.cmdTx <- secClientMsg{
// 		Type:   sagaStart,
// 		SagaID: sagaID,
// 		Ack:    ackTx,
// 	}
// 	select {
// 	case err := <-ackTx:
// 		return err
// 	case <-ctx.Done():
// 		return ctx.Err()
// 	}
// }

// // SagaList lists known sagas.
// func (c *SecClient) SagaList(ctx context.Context, marker *SagaID, limit uint) ([]*SagaView, error) {
// 	// ... (Implementation to list sagas)
// 	return nil, fmt.Errorf("SagaList not implemented")
// }

// // SagaGet fetches information about one saga.
// func (c *SecClient) SagaGet(ctx context.Context, sagaID SagaID) (*SagaView, error) {
// 	// ... (Implementation to fetch saga information)
// 	return nil, fmt.Errorf("SagaGet not implemented")
// }

// // SagaInjectError injects an error into one saga node.
// func (c *SecClient) SagaInjectError(ctx context.Context, sagaID SagaID, nodeID int) error {
// 	// ... (Implementation to inject an error)
// 	return fmt.Errorf("SagaInjectError not implemented")
// }

// // SagaInjectErrorUndo injects an error into a saga node's undo action.
// func (c *SecClient) SagaInjectErrorUndo(ctx context.Context, sagaID SagaID, nodeID int) error {
// 	// ... (Implementation to inject an error into undo action)
// 	return fmt.Errorf("SagaInjectErrorUndo not implemented")
// }

// // SagaInjectRepeat injects a node repetition into the saga.
// func (c *SecClient) SagaInjectRepeat(ctx context.Context, sagaID SagaID, nodeID int, repeat RepeatInjected) error {
// 	// ... (Implementation to inject a repeat)
// 	return fmt.Errorf("SagaInjectRepeat not implemented")
// }

// // Shutdown shuts down the SEC and waits for it to come to rest.
// func (c *SecClient) Shutdown(ctx context.Context) error {
// 	if c.shutdown {
// 		return nil
// 	}

// 	c.shutdown = true
// 	close(c.cmdTx) // signal shutdown

// 	done := make(chan struct{})
// 	go func() {
// 		c.shutdownWg.Wait()
// 		close(done)
// 	}()

// 	select {
// 	case <-done:
// 		return nil // Successful shutdown
// 	case <-ctx.Done():
// 		return ctx.Err() // Shutdown timed out or cancelled
// 	}
// }

// // SagaViewType represents the external view of a saga.
// type SagaView struct {
// 	ID    SagaID
// 	State *SagaStateView
// 	Dag   *simple.DirectedGraph
// }

// // Serialized returns a serialized representation of the SagaView.
// func (v *SagaView) Serialized() *SagaSerialized {
// 	// ... (Implementation to serialize the SagaView)
// 	return nil
// }

// // SagaStateView represents the state-specific parts of a SagaView.
// type SagaStateView struct {
// 	// ... (Fields to represent saga state)
// }

// // Status returns the SagaExecStatus for the SagaStateView.
// func (v *SagaStateView) Status() *SagaExecStatus {
// 	// ... (Implementation to get the SagaExecStatus)
// 	return nil
// }

// // RepeatInjected represents arguments for injecting node repetitions.
// type RepeatInjected struct {
// 	Action uint
// 	Undo   uint
// }

// type secClientMsgType int

// const (
// 	sagaCreate secClientMsgType = iota
// 	sagaResume
// 	sagaStart
// 	sagaList
// 	sagaGet
// 	sagaInjectError
// 	shutdown
// )

// // secClientMsg is a message passed from the SecClient to the Sec.
// type secClientMsg struct {
// 	Type           secClientMsgType
// 	SagaID         SagaID
// 	TemplateParams TemplateParams
// 	Dag            *SagaDag
// 	Marker         *SagaID
// 	Limit          uint
// 	NodeID         int
// 	ErrorType      errorInjectedType
// 	Ack            chan interface{} // Generic channel for responses
// }

// // TemplateParams is an interface for type-erased saga templates.
// type TemplateParams interface {
// 	IntoExec(ctx context.Context, log Logger, sagaID SagaID, secHdl *SecExecClient) (*SagaExecutor, error)
// }

// // SecExecClient is a handle used by SagaExecutor to communicate with the Sec.
// type SecExecClient struct {
// 	sagaID SagaID
// 	execTx chan secExecMsg
// }

// // Record writes an event to the saga log.
// func (c *SecExecClient) Record(ctx context.Context, event *SagaNodeEvent) error {
// 	ack := make(chan error, 1)
// 	c.execTx <- secExecMsg{
// 		Type:   logEvent,
// 		SagaID: c.sagaID,
// 		Event:  event,
// 		Ack:    ack,
// 	}

// 	select {
// 	case err := <-ack:
// 		return err
// 	case <-ctx.Done():
// 		return ctx.Err()
// 	}
// }

// // SagaUpdate updates the cached state for the saga.
// func (c *SecExecClient) SagaUpdate(ctx context.Context, update SagaCachedState) error {
// 	// ... (Implementation to update cached state)
// 	return fmt.Errorf("SagaUpdate not implemented")
// }

// // SagaGet fetches information about one saga.
// func (c *SecExecClient) SagaGet(ctx context.Context, sagaID SagaID) (*SagaView, error) {
// 	// ... (Implementation to fetch saga information)
// 	return nil, fmt.Errorf("SagaGet not implemented")
// }

// type secExecMsgType int

// const (
// 	sagaGet secExecMsgType = iota
// 	logEvent
// 	updateCachedState
// )

// // secExecMsg is a message passed from the SecExecClient to the Sec.
// type secExecMsg struct {
// 	Type         secExecMsgType
// 	SagaID       SagaID
// 	Event        *SagaNodeEvent
// 	UpdatedState SagaCachedState
// 	Ack          chan interface{}
// }

// // sec creates a new Saga Execution Coordinator.
// func sec(ctx context.Context, log Logger, secStore SecStore) *SecClient {
// 	cmdTx := make(chan secClientMsg, secClientMaxQueueMessages)
// 	execTx := make(chan secExecMsg, secExecMaxQueueMessages)
// 	execRx := make(chan secExecMsg, secExecMaxQueueMessages)

// 	var wg sync.WaitGroup
// 	wg.Add(1) // Add one for the sec task

// 	sec := &Sec{
// 		log:        log,
// 		sagas:      make(map[SagaID]*Saga),
// 		secStore:   secStore,
// 		tasks:      NewTaskManager(),
// 		cmdRx:      cmdTx,
// 		execTx:     execTx,
// 		execRx:     execRx,
// 		shutdown:   false,
// 		shutdownWg: &wg,
// 	}

// 	go func() {
// 		defer wg.Done()
// 		sec.run(ctx)
// 	}()

// 	return &SecClient{
// 		cmdTx:      cmdTx,
// 		task:       nil, // Will be populated when a saga is created
// 		shutdown:   false,
// 		shutdownWg: &wg,
// 	}
// }

// // run is the main loop of the SEC.
// func (s *Sec) run(ctx context.Context) {
// 	s.log.Info("SEC running")
// 	for {
// 		if s.shutdown && s.tasks.IsEmpty() {
// 			s.log.Info("SEC shutting down")
// 			close(s.execTx) // Signal executors to stop
// 			return
// 		}

// 		select {
// 		case <-ctx.Done():
// 			s.log.Info("SEC context cancelled, shutting down")
// 			s.shutdown = true
// 		case msg, ok := <-s.cmdRx:
// 			if !ok {
// 				// cmdRx closed, initiate shutdown
// 				s.shutdown = true
// 				continue
// 			}
// 			s.dispatchClientMessage(ctx, msg)
// 		case msg := <-s.execRx:
// 			s.dispatchExecMessage(ctx, msg)
// 		case task := <-s.tasks.Completed():
// 			s.handleCompletedTask(ctx, task)
// 		}
// 	}
// }

// // Saga represents the internal state of a saga.
// type Saga struct {
// 	ID       SagaID
// 	Log      Logger
// 	Dag      *simple.DirectedGraph
// 	RunState *SagaRunState
// }

// // SagaRunState represents the runtime state of a saga.
// type SagaRunState struct {
// 	ExecState      SagaCachedState
// 	Stopping       bool
// 	QueueTodo      []int
// 	QueueUndo      []int
// 	NodeTasks      map[int]*Task
// 	NodeOutputs    map[int]ActionData
// 	NodesUndone    map[int]undoMode
// 	NodeErrors     map[int]error
// 	UndoErrors     map[int]error
// 	Sglog          *SagaLog
// 	InjectedErrors map[int]errorInjectedType
// 	SecHdl         *SecExecClient
// }

// // SecStep represents the next step an SEC needs to take.
// type SecStep struct {
// 	Type       secStepType
// 	SagaID     SagaID
// 	Result     *SagaResult
// 	Status     *SagaExecStatus
// 	InsertData *sagaInsertData
// 	DoneData   *sagaDoneData
// }

// type secStepType int

// const (
// 	sagaInsert secStepType = iota
// 	sagaDone
// )

// // SagaSerialized represents a serialized saga.
// type SagaSerialized struct {
// 	SagaID SagaID
// 	Dag    *simple.DirectedGraph
// 	Events []*SagaNodeEvent
// }

// func (s *Sec) dispatchClientMessage(ctx context.Context, msg secClientMsg) {
// 	switch msg.Type {
// 	case sagaCreate:
// 		s.cmdSagaCreate(ctx, msg)
// 	case sagaResume:
// 		s.cmdSagaResume(ctx, msg)
// 	case sagaStart:
// 		s.cmdSagaStart(ctx, msg)
// 	case sagaList:
// 		s.cmdSagaList(ctx, msg)
// 	case sagaGet:
// 		s.cmdSagaGet(ctx, msg)
// 	case sagaInjectError:
// 		s.cmdSagaInjectError(ctx, msg)
// 	case shutdown:
// 		s.cmdShutdown()
// 	default:
// 		// TODO: Handle unknown message types
// 		s.log.Warnf("Unknown client message type: %d", msg.Type)
// 	}
// }

// func (s *Sec) dispatchExecMessage(ctx context.Context, msg secExecMsg) {
// 	switch msg.Type {
// 	case sagaGet:
// 		s.execSagaGet(msg)
// 	case logEvent:
// 		s.execLogEvent(ctx, msg)
// 	case updateCachedState:
// 		s.execUpdateCachedState(ctx, msg)
// 	default:
// 		// TODO: Handle unknown message types
// 		s.log.Warnf("Unknown executor message type: %d", msg.Type)
// 	}
// }

// func (s *Sec) handleCompletedTask(ctx context.Context, task *Task) {
// 	// ... (Handle completed tasks)
// 	fmt.Println("Task completed:", task)
// }

// func (s *Sec) cmdSagaCreate(ctx context.Context, msg secClientMsg) {
// 	// ... (Implementation to create a new saga)
// 	s.doSagaCreate(ctx, msg.Ack, msg.SagaID, msg.TemplateParams, msg.Dag, false)
// }

// func (s *Sec) cmdSagaResume(ctx context.Context, msg secClientMsg) {
// 	// ... (Implementation to resume a saga)
// 	fmt.Errorf("SagaResume not implemented")
// }

// func (s *Sec) cmdSagaStart(ctx context.Context, msg secClientMsg) {
// 	// ... (Implementation to start a saga)
// 	sagaID := msg.SagaID
// 	result := s.doSagaStart(ctx, sagaID)
// 	sendResult(s.log, msg.Ack, result)
// }

// func (s *Sec) cmdSagaList(ctx context.Context, msg secClientMsg) {
// 	// ... (Implementation to list sagas)
// 	fmt.Errorf("SagaList not implemented")
// }

// func (s *Sec) cmdSagaGet(ctx context.Context, msg secClientMsg) {
// 	// ... (Implementation to fetch saga information)
// 	fmt.Errorf("SagaGet not implemented")
// }

// func (s *Sec) cmdSagaInjectError(ctx context.Context, msg secClientMsg) {
// 	// ... (Implementation to inject an error)
// 	fmt.Errorf("SagaInjectError not implemented")
// }

// func (s *Sec) cmdShutdown() {
// 	// ... (Implementation to shutdown the SEC)
// 	s.shutdown = true
// }

// func (s *Sec) execSagaGet(msg secExecMsg) {
// 	// ... (Implementation to fetch saga information from executor)
// 	fmt.Errorf("SagaGet not implemented")
// }

// func (s *Sec) execLogEvent(ctx context.Context, msg secExecMsg) {
// 	// ... (Implementation to record an event from executor)
// 	event := msg.Event
// 	s.log.Debugf("Saga log event: %+v", event)
// 	// TODO: Persist event using s.secStore
// 	sendResult(s.log, msg.Ack, nil)
// }

// func (s *Sec) execUpdateCachedState(ctx context.Context, msg secExecMsg) {
// 	// ... (Implementation to update cached state from executor)
// 	update := msg.UpdatedState
// 	sagaID := msg.SagaID
// 	s.log.Infof("Update for saga cached state: saga_id=%s, new_state=%v", sagaID, update)
// 	// TODO: Update cached state using s.secStore
// 	sendResult(s.log, msg.Ack, nil)
// }

// func (s *Sec) doSagaCreate(ctx context.Context, ack chan interface{}, sagaID SagaID, templateParams TemplateParams, dag *SagaDag, autostart bool) {
// 	//TODO: Implement doSagaCreate
// 	s.log.Infof("Saga create: saga_id=%s, saga_name=%s", sagaID, dag.Name)

// 	// TODO: Create persistent record using s.secStore

// 	// TODO: Implement Saga struct and SagaRunState struct
// 	runState := &SagaRunState{}
// 	saga := &Saga{
// 		ID:       sagaID,
// 		Log:      s.log,
// 		Dag:      dag.Graph,
// 		RunState: runState,
// 	}

// 	if _, exists := s.sagas[sagaID]; exists {
// 		sendResult(s.log, ack, fmt.Errorf("saga_id %s cannot be inserted; already in use", sagaID))
// 		return
// 	}

// 	s.sagas[sagaID] = saga

// 	if autostart {
// 		if err := s.doSagaStart(ctx, sagaID); err != nil {
// 			sendResult(s.log, ack, fmt.Errorf("failed to autostart saga: %w", err))
// 			return
// 		}
// 	}

// 	task := s.tasks.NewTask(sagaID) // Use TaskManager to manage the saga task

// 	// Send back the task for the client to wait on
// 	sendResult(s.log, ack, task)
// }

// func (s *Sec) doSagaStart(ctx context.Context, sagaID SagaID) error {
// 	// TODO: Implement doSagaStart
// 	if _, ok := s.sagas[sagaID]; !ok {
// 		return fmt.Errorf("no such saga: %s", sagaID)
// 	}

// 	// ... (Logic to start or resume the saga)

// 	return fmt.Errorf("doSagaStart not implemented")
// }

// func (s *Sec) sagaLookup(sagaID SagaID) (*Saga, error) {
// 	//TODO: Implement sagaLookup
// 	if saga, ok := s.sagas[sagaID]; ok {
// 		return saga, nil
// 	}
// 	return nil, fmt.Errorf("no such saga: %s", sagaID)
// }

// func (s *Sec) sagaRemove(sagaID SagaID) (*Saga, error) {
// 	//TODO: Implement sagaRemove
// 	if saga, ok := s.sagas[sagaID]; ok {
// 		delete(s.sagas, sagaID)
// 		return saga, nil
// 	}
// 	return nil, fmt.Errorf("no such saga: %s", sagaID)
// }

// func sendResult(log Logger, ackCh chan interface{}, result interface{}) {
// 	//TODO: Implement sendResult
// 	if ackCh == nil {
// 		return // No response channel, ignore result
// 	}
// 	select {
// 	case ackCh <- result:
// 	default:
// 		// Ack channel is full or closed, log a warning
// 		log.Warn("Failed to send result to client: ack channel is full or closed")
// 	}
// }

// // Task represents a running saga or asynchronous operation.
// type Task struct {
// 	ID     SagaID
// 	Done   chan struct{}
// 	Result chan *SagaResult
// }

// // NewTask creates a new Task.
// func (t *TaskManager) NewTask(id SagaID) *Task {
// 	task := &Task{
// 		ID:     id,
// 		Done:   make(chan struct{}),
// 		Result: make(chan *SagaResult, 1),
// 	}
// 	t.tasks[id] = task
// 	t.wg.Add(1)
// 	return task
// }

// // TaskManager manages running tasks.
// type TaskManager struct {
// 	tasks map[SagaID]*Task
// 	wg    sync.WaitGroup
// 	mu    sync.Mutex
// 	done  chan *Task // Channel to signal task completion
// }

// // NewTaskManager creates a new TaskManager.
// func NewTaskManager() *TaskManager {
// 	return &TaskManager{
// 		tasks: make(map[SagaID]*Task),
// 		done:  make(chan *Task),
// 	}
// }

// // Completed returns a channel for receiving completed tasks.
// func (t *TaskManager) Completed() <-chan *Task {
// 	return t.done
// }

// // IsEmpty checks if there are any running tasks.
// func (t *TaskManager) IsEmpty() bool {
// 	t.mu.Lock()
// 	defer t.mu.Unlock()
// 	return len(t.tasks) == 0
// }
