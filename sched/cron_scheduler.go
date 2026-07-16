package sched

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ojv/cog/log"
)

const (
	OnMode  = true
	OffMode = false
)

var (
	ErrNotFoundJob      = errors.New("not found job")
	ErrAlreadyRegister  = errors.New("the job already in pool")
	ErrJobDOFuncNil     = errors.New("callback func is nil")
	ErrCronSpecInvalid  = errors.New("crontab spec is invalid")
	ErrSchedulerStopped = errors.New("cron scheduler already stopped")
)

// null logger
//var defaultLogger = func(level, s string) {}

type loggerType func(level, s string)

func SetLogger(logger loggerType) {
	//defaultLogger = logger
}

// panic call
var panicCaller = func(srv, err string) {
}

type panicType func(srv, err string)

func SetPanicCaller(p panicType) {
	panicCaller = p
}

// NewCron - create CronSchduler
func NewCron() *CronSchduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &CronSchduler{
		tasks:  make(map[string]*JobModel),
		ctx:    ctx,
		cancel: cancel,
		wg:     &sync.WaitGroup{},
		once:   &sync.Once{},
	}
}

// CronSchduler
type CronSchduler struct {
	tasks  map[string]*JobModel
	ctx    context.Context
	cancel context.CancelFunc

	wg      *sync.WaitGroup
	once    *sync.Once
	stopped atomic.Bool

	sync.RWMutex
}

// Register - only register srv's job model, don't start auto.
func (c *CronSchduler) Register(srv string, model *JobModel) error {
	return c.reset(srv, model, true, false)
}

// UpdateJobModel - stop old job, update srv's job model
func (c *CronSchduler) UpdateJobModel(srv string, model *JobModel) error {
	return c.reset(srv, model, false, true)
}

// DynamicRegister - after cronlib already run, dynamic add a job, the job autostart by cronlib.
func (c *CronSchduler) DynamicRegister(srv string, model *JobModel) error {
	return c.reset(srv, model, false, true)
}

// reset - reset srv model
func (c *CronSchduler) reset(srv string, model *JobModel, denyReplace, autoStart bool) error {
	if c.stopped.Load() {
		return ErrSchedulerStopped
	}

	c.Lock()
	defer c.Unlock()

	// double-check under lock to avoid race with Stop()
	if c.stopped.Load() {
		return ErrSchedulerStopped
	}

	// validate model
	err := model.validate()
	if err != nil {
		return err
	}

	cctx, cancel := context.WithCancel(c.ctx)
	model.ctx = cctx
	model.cancel = cancel
	model.srv = srv

	oldModel, ok := c.tasks[srv]
	if denyReplace && ok {
		return ErrAlreadyRegister
	}

	if ok {
		oldModel.kill()
	}

	c.tasks[srv] = model
	if autoStart {
		c.wg.Add(1)
		go c.tasks[srv].runLoop(c.wg)
	}

	return nil
}

// UnRegister - stop and delete srv
func (c *CronSchduler) UnRegister(srv string) error {
	c.Lock()
	defer c.Unlock()

	oldModel, ok := c.tasks[srv]
	if !ok {
		return ErrNotFoundJob
	}

	oldModel.kill()
	delete(c.tasks, srv)
	return nil
}

// Stop - stop all cron job. After Stop, the scheduler is terminal:
// any subsequent Register/DynamicRegister/UpdateJobModel returns ErrSchedulerStopped.
func (c *CronSchduler) Stop() {
	c.Lock()
	defer c.Unlock()

	c.stopped.Store(true)

	for srv, job := range c.tasks {
		job.kill()
		delete(c.tasks, srv)
	}
	c.cancel()
}

// StopService - stop job by serviceName
func (c *CronSchduler) StopService(srv string) {
	c.Lock()
	defer c.Unlock()

	job, ok := c.tasks[srv]
	if !ok {
		return
	}

	job.kill()
	delete(c.tasks, srv)
}

// StopServicePrefix - stop job by srv regex prefix.
// if regex = "risk.scan", stop risk.scan.total, risk.scan.user at the same time
func (c *CronSchduler) StopServicePrefix(regex string) {
	c.Lock()
	defer c.Unlock()

	// regex match
	for srv, job := range c.tasks {
		if !strings.HasPrefix(srv, regex) {
			continue
		}

		job.kill()
		delete(c.tasks, srv)
	}
}

func validateSpec(spec string) bool {
	_, err := ParseCron(spec)
	if err != nil {
		return false
	}

	return true
}

func getNextDue(spec string) (time.Time, error) {
	sc, err := ParseCron(spec)
	if err != nil {
		return time.Now(), fmt.Errorf("解析cron表达式错误: %w", err)
	}

	now := time.Now()
	due := sc.Next(now)
	return due, nil
}

// 优化 getNextDueSafe 函数，减少重试次数和日志输出
func getNextDueSafe(spec string, last time.Time) (time.Time, error) {
	var (
		due        time.Time
		err        error
		maxRetries = 5 // 减少最大重试次数
	)

	// 如果上次执行时间为零，直接获取下次执行时间
	if last.IsZero() {
		return getNextDue(spec)
	}

	// 使用指数退避策略
	for retryCount := 0; retryCount < maxRetries; retryCount++ {
		due, err = getNextDue(spec)
		if err != nil {
			log.Errorf("获取下次执行时间失败: %v", err)
			return due, err
		}

		if due.Sub(last) > 0 {
			return due, nil
		}

		// 只在第一次和最后一次重试时记录日志
		if retryCount == 0 || retryCount == maxRetries-1 {
			log.Warnf("计算的下次执行时间 %v 不大于上次执行时间 %v，重试 %d/%d",
				due, last, retryCount+1, maxRetries)
		}

		// 指数退避，避免CPU空转
		time.Sleep(time.Millisecond * time.Duration(1<<uint(retryCount)))
	}

	err = fmt.Errorf("无法获取有效的下次执行时间，已重试%d次", maxRetries)
	log.Error(err.Error())
	return due, err
}

func (c *CronSchduler) Start() {
	// only once call
	c.once.Do(func() {
		c.RLock()
		defer c.RUnlock()

		for _, job := range c.tasks {
			c.wg.Add(1)
			job.runLoop(c.wg)
		}

	})
}

// Wait - if all jobs is exited, return.
func (c *CronSchduler) Wait() {
	c.wg.Wait()
}

// WaitStop - when stop cronlib controller, return.
func (c *CronSchduler) WaitStop() {
	select {
	case <-c.ctx.Done():
	}
}

func (c *CronSchduler) GetServiceCron(srv string) (*JobModel, error) {
	c.RLock()
	defer c.RUnlock()

	oldModel, ok := c.tasks[srv]
	if !ok {
		return nil, ErrNotFoundJob
	}

	return oldModel, nil
}

// NewJobModel - defualt block sync callfunc
func NewJobModel(spec string, f func(), options ...JobOption) (*JobModel, error) {
	job := &JobModel{
		async:      false,
		do:         f,
		spec:       spec,
		notifyChan: make(chan int, 1),
	}
	job.running.Store(true)

	for _, opt := range options {
		if opt != nil {
			if err := opt(job); err != nil {
				return nil, err
			}
		}
	}

	err := job.validate()
	if err != nil {
		return nil, err
	}

	return job, nil
}

// 在 kill 方法中添加对象回收
func (j *JobModel) kill() {
	j.running.Store(false)
	j.exited.Store(true)
	if j.cancel != nil {
		j.cancel()
	}
}

type JobOption func(*JobModel) error

func AsyncMode() JobOption {
	return func(o *JobModel) error {
		o.async = true
		return nil
	}
}

func TryCatchMode() JobOption {
	return func(o *JobModel) error {
		o.tryCatch = true
		return nil
	}
}

type JobModel struct {
	// srv name
	srv string

	// callfunc
	do func()

	// if async = true; go func() { do() }
	async bool

	// try catch panic
	tryCatch bool

	// cron spec
	spec string

	// for control
	ctx        context.Context
	cancel     context.CancelFunc
	notifyChan chan int

	// break for { ... } loop
	running atomic.Bool

	// ensure job worker is exited already
	exited atomic.Bool

	// 防止任务并发执行（上次超时未完成时跳过本次）
	taskRunning atomic.Bool

	// 添加统计信息
	execCount   int64     // 执行次数
	lastExecAt  time.Time // 上次执行时间
	lastCostMs  int64     // 上次执行耗时(毫秒)
	totalCostMs int64     // 总执行耗时(毫秒)

	sync.RWMutex
}

func (j *JobModel) SetTryCatch(b bool) {
	j.tryCatch = b
}

func (j *JobModel) SetAsyncMode(b bool) {
	j.async = b
}

func (j *JobModel) validate() error {
	if j.do == nil {
		return ErrJobDOFuncNil
	}

	if _, err := getNextDue(j.spec); err != nil {
		return err
	}

	return nil
}

func (j *JobModel) runLoop(wg *sync.WaitGroup) {
	go j.run(wg)
}

// 优化 run 方法，添加超时控制和性能统计
func (j *JobModel) run(wg *sync.WaitGroup) {
	defer wg.Done()

	// 设置默认超时时间
	defaultTimeout := 30 * time.Second

	for j.running.Load() {
		// 计算下次执行时间
		j.RLock()
		lastExec := j.lastExecAt
		j.RUnlock()
		due, err := getNextDueSafe(j.spec, lastExec)
		if err != nil {
			logError("计算下次执行时间失败: %v", err)
			time.Sleep(time.Second) // 出错后短暂等待
			continue
		}

		// 计算等待时间
		waitTime := due.Sub(time.Now())
		if waitTime < 0 {
			waitTime = 0
		}

		// 等待下次执行
		select {
		case <-j.ctx.Done():
			j.exited.Store(true)
			return
		case <-time.After(waitTime):
			// 跳过本次执行如果上次任务仍在运行（防止 goroutine 堆积和数据竞争）
			if !j.taskRunning.CompareAndSwap(false, true) {
				logWarn("任务 %s 上次执行未完成，跳过本次执行", j.srv)
				continue
			}

			// 执行任务
			startTime := time.Now()
			j.Lock()
			j.lastExecAt = startTime
			j.execCount++
			j.Unlock()

			logInfo("任务开始执行: %s", j.srv)

			if j.async {
				// 异步模式：不等待完成，直接进入下次调度。
				// stats 在 goroutine 中更新；无超时控制（调用者自行处理）。
				go func() {
					defer j.taskRunning.Store(false)
					if j.tryCatch {
						tryCatch(j)
					} else {
						j.do()
					}
					costMs := time.Since(startTime).Milliseconds()
					j.Lock()
					j.lastCostMs = costMs
					j.totalCostMs += costMs
					j.Unlock()
					logInfo("任务执行完成(异步): %s, 耗时: %dms", j.srv, costMs)
				}()
			} else {
				// 同步模式：等待完成或超时
				done := make(chan struct{})
				go func() {
					defer j.taskRunning.Store(false)
					defer close(done)
					if j.tryCatch {
						tryCatch(j)
					} else {
						j.do()
					}
				}()

				// 等待任务完成或超时
				select {
				case <-done:
					// 任务正常完成
				case <-time.After(defaultTimeout):
					logWarn("任务执行超时: %s", j.srv)
				}

				// 计算执行耗时
				costMs := time.Since(startTime).Milliseconds()
				j.Lock()
				j.lastCostMs = costMs
				j.totalCostMs += costMs
				j.Unlock()

				logInfo("任务执行完成: %s, 耗时: %dms", j.srv, costMs)
			}
		case <-j.notifyChan:
			// 收到通知，立即执行
			continue
		}
	}

	j.exited.Store(true)
}

func (j *JobModel) workerExited() bool {
	return j.exited.Load()
}

func (j *JobModel) notifySig() {
	select {
	case j.notifyChan <- 1:
	default:
		// avoid block
		return
	}
}

func tryCatch(job *JobModel) {
	defer func() {
		if e := recover(); e != nil {
			panicCaller(
				job.srv,
				fmt.Sprintf("%v", e),
			)

			log.Errorf("srv: %s, trycatch panicing %v", job.srv, e)
		}
	}()

	job.do()
}

// HealthCheck - 检查所有任务的健康状态
func (c *CronSchduler) HealthCheck() map[string]map[string]interface{} {
	c.RLock()
	defer c.RUnlock()

	result := make(map[string]map[string]interface{})
	now := time.Now()

	for srv, job := range c.tasks {
		job.RLock()
		status := map[string]interface{}{
			"running":     job.running.Load(),
			"exited":      job.exited.Load(),
			"spec":        job.spec,
			"execCount":   job.execCount,
			"lastExecAt":  job.lastExecAt,
			"lastCostMs":  job.lastCostMs,
			"totalCostMs": job.totalCostMs,
			"avgCostMs":   int64(0),
		}

		if job.execCount > 0 {
			status["avgCostMs"] = job.totalCostMs / job.execCount
		}

		// 检查任务是否应该运行但没有运行
		if job.running.Load() && !job.exited.Load() && !job.lastExecAt.IsZero() {
			nextDue, err := getNextDue(job.spec)
			if err == nil {
				status["nextDue"] = nextDue
				status["overdue"] = now.After(nextDue) && now.Sub(nextDue) > time.Minute
			}
		}
		job.RUnlock()

		result[srv] = status
	}

	return result
}

// 添加日志级别控制
var logLevel = 0 // 0=all, 1=warn+error, 2=error only

func SetLogLevel(level int) {
	logLevel = level
}

// 优化日志函数
func logInfo(format string, args ...interface{}) {
	if logLevel <= 0 {
		log.Infof(format, args...)
	}
}

func logWarn(format string, args ...interface{}) {
	if logLevel <= 1 {
		log.Warnf(format, args...)
	}
}

func logError(format string, args ...interface{}) {
	log.Errorf(format, args...)
}
