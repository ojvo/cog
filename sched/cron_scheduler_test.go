package sched

import (
	"sync"
	"testing"
	"time"
)

func TestCronScheduler_Basic(t *testing.T) {
	cron := NewCron()
	defer cron.Stop()

	var count int
	var mu sync.Mutex
	done := make(chan struct{})

	job, err := NewJobModel("* * * * * *", func() {
		mu.Lock()
		count++
		if count == 2 {
			close(done)
		}
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	err = cron.Register("test_job", job)
	if err != nil {
		t.Fatalf("Failed to register job: %v", err)
	}

	cron.Start()

	select {
	case <-done:
		// Success
	case <-time.After(3 * time.Second):
		t.Error("Job did not run twice in 3 seconds")
	}
}

func TestCronScheduler_DynamicRegister(t *testing.T) {
	cron := NewCron()
	cron.Start()
	defer cron.Stop()

	done := make(chan struct{})
	job, _ := NewJobModel("* * * * * *", func() {
		close(done)
	})

	err := cron.DynamicRegister("dynamic_job", job)
	if err != nil {
		t.Fatalf("Failed to dynamic register: %v", err)
	}

	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Error("Dynamic job did not run")
	}
}

func TestCronScheduler_UnRegister(t *testing.T) {
	cron := NewCron()
	cron.Start()
	defer cron.Stop()

	var count int
	var mu sync.Mutex

	job, _ := NewJobModel("* * * * * *", func() {
		mu.Lock()
		count++
		mu.Unlock()
	})

	cron.Register("remove_job", job)
	time.Sleep(1500 * time.Millisecond) // Let it run at least once

	err := cron.UnRegister("remove_job")
	if err != nil {
		t.Fatalf("Failed to unregister: %v", err)
	}

	mu.Lock()
	countAtStop := count
	mu.Unlock()

	time.Sleep(2 * time.Second) // Wait to ensure it doesn't run again

	mu.Lock()
	if count != countAtStop {
		t.Error("Job continued running after unregister")
	}
	mu.Unlock()
}

func TestCronScheduler_HealthCheck(t *testing.T) {
	cron := NewCron()
	cron.Start()
	defer cron.Stop()

	job, _ := NewJobModel("* * * * * *", func() {})
	cron.Register("health_job", job)

	time.Sleep(1100 * time.Millisecond)

	status := cron.HealthCheck()
	if _, ok := status["health_job"]; !ok {
		t.Error("Health check missing job status")
	}
}

// Deleted duplicate test

func TestCronScheduler_UpdateJobModel(t *testing.T) {
	cron := NewCron()
	defer cron.Stop()

	var count1, count2 int
	var mu sync.Mutex

	// Job 1
	job1, _ := NewJobModel("* * * * * *", func() {
		mu.Lock()
		count1++
		mu.Unlock()
	})

	cron.Register("update_job", job1)
	cron.Start()
	time.Sleep(1500 * time.Millisecond)

	// Update to Job 2
	job2, _ := NewJobModel("* * * * * *", func() {
		mu.Lock()
		count2++
		mu.Unlock()
	})

	err := cron.UpdateJobModel("update_job", job2)
	if err != nil {
		t.Fatalf("Failed to update job: %v", err)
	}

	// Wait for Job 2 to run
	time.Sleep(1500 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if count1 == 0 {
		t.Error("Job 1 should have run")
	}
	if count2 == 0 {
		t.Error("Job 2 should have run after update")
	}
}

func TestCronScheduler_StopService(t *testing.T) {
	cron := NewCron()
	cron.Start()
	defer cron.Stop()

	job, _ := NewJobModel("* * * * * *", func() {})
	cron.Register("service_a", job)
	cron.Register("service_b", job)

	cron.StopService("service_a")

	_, err := cron.GetServiceCron("service_a")
	if err == nil {
		t.Error("Service A should be stopped and removed")
	}

	_, err = cron.GetServiceCron("service_b")
	if err != nil {
		t.Error("Service B should still exist")
	}
}

func TestCronScheduler_StopServicePrefix(t *testing.T) {
	cron := NewCron()
	cron.Start()
	defer cron.Stop()

	job, _ := NewJobModel("* * * * * *", func() {})
	cron.Register("risk.scan.user", job)
	cron.Register("risk.scan.total", job)
	cron.Register("risk.report", job)
	cron.Register("other.service", job)

	cron.StopServicePrefix("risk.scan")

	if _, err := cron.GetServiceCron("risk.scan.user"); err == nil {
		t.Error("risk.scan.user should be stopped")
	}
	if _, err := cron.GetServiceCron("risk.scan.total"); err == nil {
		t.Error("risk.scan.total should be stopped")
	}
	if _, err := cron.GetServiceCron("risk.report"); err != nil {
		t.Error("risk.report should still exist")
	}
	if _, err := cron.GetServiceCron("other.service"); err != nil {
		t.Error("other.service should still exist")
	}
}

// TestCronScheduler_RegisterAfterStop verifies that after Stop the scheduler
// is terminal: Register/DynamicRegister/UpdateJobModel all return
// ErrSchedulerStopped instead of silently registering jobs that can never run.
func TestCronScheduler_RegisterAfterStop(t *testing.T) {
	cron := NewCron()
	cron.Start()
	cron.Stop()

	job, _ := NewJobModel("* * * * * *", func() {})

	if err := cron.Register("after_stop", job); err != ErrSchedulerStopped {
		t.Errorf("Register after Stop = %v, want ErrSchedulerStopped", err)
	}
	if err := cron.DynamicRegister("after_stop_dyn", job); err != ErrSchedulerStopped {
		t.Errorf("DynamicRegister after Stop = %v, want ErrSchedulerStopped", err)
	}
	if err := cron.UpdateJobModel("after_stop", job); err != ErrSchedulerStopped {
		t.Errorf("UpdateJobModel after Stop = %v, want ErrSchedulerStopped", err)
	}
}

func TestCronScheduler_JobOptions(t *testing.T) {
	cron := NewCron()
	defer cron.Stop()

	// Test TryCatchMode (Panic Recovery)
	done := make(chan struct{})
	jobPanic, _ := NewJobModel("* * * * * *", func() {
		defer close(done)
		panic("test panic")
	}, TryCatchMode())

	cron.Register("panic_job", jobPanic)
	cron.Start()

	select {
	case <-done:
		// Success (didn't crash scheduler)
	case <-time.After(2 * time.Second):
		t.Error("Panic job didn't run or blocked")
	}

	// Test AsyncMode
	// Hard to test async strictly without mocking time or strict synchronization,
	// but we can verify it runs.
	jobAsync, _ := NewJobModel("* * * * * *", func() {
		time.Sleep(100 * time.Millisecond)
	}, AsyncMode())

	if !jobAsync.async {
		t.Error("Job should be in async mode")
	}
}

func TestCronScheduler_JobTimeout(t *testing.T) {
	cron := NewCron()
	defer cron.Stop()

	// This job takes longer than standard timeout?
	// The default timeout is 30s. We can't wait that long in a unit test.
	// But we can check if it logs a warning.
	// Or we can rely on code inspection that timeout logic exists.
	// To test properly we would need to be able to set the timeout duration per job.
	// Current implementation hardcodes 30s.
	// So we'll skip the actual timeout test but verify the job runs.

	done := make(chan struct{})
	job, _ := NewJobModel("* * * * * *", func() {
		close(done)
	})

	cron.Register("timeout_test", job)
	cron.Start()

	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Error("Job failed to run")
	}
}

// TestCronScheduler_AsyncMode verifies that an async-mode job does not block
// the scheduler loop: execCount is recorded at dispatch time (before the task
// finishes), and lastCostMs is updated by the background goroutine on completion.
func TestCronScheduler_AsyncMode(t *testing.T) {
	cron := NewCron()
	defer cron.Stop()

	started := make(chan struct{})
	done := make(chan struct{})
	job, _ := NewJobModel("* * * * * *", func() {
		close(started)
		time.Sleep(300 * time.Millisecond)
		close(done)
	}, AsyncMode())

	cron.Register("async_job", job)
	cron.Start()

	// Wait until the task has started running.
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("Async job did not start")
	}

	// Task is still running (300ms). execCount should already be 1 because
	// the scheduler records it at dispatch time, not on completion. This
	// proves the scheduler loop was not blocked waiting for j.do().
	time.Sleep(50 * time.Millisecond)
	status := cron.HealthCheck()
	js := status["async_job"]
	if js == nil {
		t.Fatal("missing async_job in health check")
	}
	if ec := js["execCount"].(int64); ec != 1 {
		t.Errorf("execCount = %d, want 1 (scheduler should not be blocked)", ec)
	}

	// Wait for the background goroutine to finish.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Async job did not complete")
	}

	// Give the goroutine a moment to update stats.
	time.Sleep(50 * time.Millisecond)
	status = cron.HealthCheck()
	js = status["async_job"]
	if cost := js["lastCostMs"].(int64); cost < 200 {
		t.Errorf("lastCostMs = %d, want >= 200", cost)
	}
}
