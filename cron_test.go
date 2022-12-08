package cron

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Many tests schedule a job for every second, and then wait at most a second
// for it to run.  This amount is just slightly larger than 1 second to
// compensate for a few milliseconds of runtime.
const OneSecond = 1*time.Second + 50*time.Millisecond

type syncWriter struct {
	wr bytes.Buffer
	m  sync.Mutex
}

func (sw *syncWriter) Write(data []byte) (n int, err error) {
	sw.m.Lock()
	n, err = sw.wr.Write(data)
	sw.m.Unlock()
	return
}

func (sw *syncWriter) String() string {
	sw.m.Lock()
	defer sw.m.Unlock()
	return sw.wr.String()
}

func newBufLogger(sw *syncWriter) Logger {
	return PrintfLogger(log.New(sw, "", log.LstdFlags))
}

func TestFuncPanicRecovery(t *testing.T) {
	var buf syncWriter
	cron := New(WithParser(secondParser),
		WithChain(Recover(newBufLogger(&buf))))
	cron.Start(context.TODO())
	defer cron.Stop(context.TODO())
	cron.AddFunc("TestFuncPanicRecovery", "* * * * * ?", func(context.Context) error {
		panic("YOLO")
	})

	<-time.After(OneSecond)
	if !strings.Contains(buf.String(), "YOLO") {
		t.Error("expected a panic to be logged, got none")
	}

}

type DummyJob struct{}

func (d DummyJob) Run(context.Context) error {
	panic("YOLO")
}

func TestJobPanicRecovery(t *testing.T) {
	var job DummyJob

	var buf syncWriter
	cron := New(WithParser(secondParser),
		WithChain(Recover(newBufLogger(&buf))))
	cron.Start(context.TODO())
	defer cron.Stop(context.TODO())
	cron.AddJob("TestJobPanicRecovery", "* * * * * ?", job)

	<-time.After(OneSecond)
	if !strings.Contains(buf.String(), "YOLO") {
		t.Error("expected a panic to be logged, got none")
	}

}

// Start and stop cron with no entries.
func TestNoEntries(t *testing.T) {
	cron := newWithSeconds()
	cron.Start(context.TODO())

	select {
	case <-time.After(OneSecond):
		t.Fatal("expected cron will be stopped immediately")
	case <-stop(cron):
	}
}

// Start, stop, then add an entry. Verify entry doesn't run.
func TestStopCausesJobsToNotRun(t *testing.T) {
	wg := &sync.WaitGroup{}
	wg.Add(1)

	cron := newWithSeconds()
	cron.Start(context.TODO())
	cron.Stop(context.TODO())
	cron.AddFunc("TestStopCausesJobsToNotRun", "* * * * * ?", func(context.Context) error {
		wg.Done()
		return nil
	})

	select {
	case <-time.After(OneSecond):
		// No job ran!
	case <-wait(wg):
		t.Fatal("expected stopped cron does not run any job")
	}
}

// Add a job, start cron, expect it runs.
func TestAddBeforeRunning(t *testing.T) {
	wg := &sync.WaitGroup{}
	wg.Add(1)

	cron := newWithSeconds()
	cron.AddFunc("TestAddBeforeRunning", "* * * * * ?", func(context.Context) error {
		wg.Done()
		return nil
	})
	cron.Start(context.TODO())
	defer cron.Stop(context.TODO())

	// Give cron 2 seconds to run our job (which is always activated).
	select {
	case <-time.After(OneSecond):
		t.Fatal("expected job runs")
	case <-wait(wg):
	}
}

// Start cron, add a job, expect it runs.
func TestAddWhileRunning(t *testing.T) {
	wg := &sync.WaitGroup{}
	wg.Add(1)

	cron := newWithSeconds()
	cron.Start(context.TODO())
	defer cron.Stop(context.TODO())
	cron.AddFunc("TestAddWhileRunning", "* * * * * ?", func(context.Context) error {
		wg.Done()
		return nil
	})

	select {
	case <-time.After(OneSecond):
		t.Fatal("expected job runs")
	case <-wait(wg):
	}
}

// Test for #34. Adding a job after calling start results in multiple job invocations
func TestAddWhileRunningWithDelay(t *testing.T) {
	cron := newWithSeconds()
	cron.Start(context.TODO())
	defer cron.Stop(context.TODO())
	time.Sleep(5 * time.Second)
	var calls int64
	cron.AddFunc("TestAddWhileRunning", "* * * * * *", func(context.Context) error {
		atomic.AddInt64(&calls, 1)
		return nil
	})

	<-time.After(OneSecond)
	if atomic.LoadInt64(&calls) != 1 {
		t.Errorf("called %d times, expected 1\n", calls)
	}
}

// Add a job, remove a job, start cron, expect nothing runs.
func TestRemoveBeforeRunning(t *testing.T) {
	wg := &sync.WaitGroup{}
	wg.Add(1)

	cron := newWithSeconds()
	id, _ := cron.AddFunc("TestRemoveBeforeRunning", "* * * * * ?", func(context.Context) error {
		wg.Done()
		return nil
	})
	cron.Remove(id)
	cron.Start(context.TODO())
	defer cron.Stop(context.TODO())

	select {
	case <-time.After(OneSecond):
		// Success, shouldn't run
	case <-wait(wg):
		t.FailNow()
	}
}

// Start cron, add a job, remove it, expect it doesn't run.
func TestRemoveWhileRunning(t *testing.T) {
	wg := &sync.WaitGroup{}
	wg.Add(1)

	cron := newWithSeconds()
	cron.Start(context.TODO())
	defer cron.Stop(context.TODO())
	id, _ := cron.AddFunc("TestRemoveWhileRunning", "* * * * * ?", func(context.Context) error {
		wg.Done()
		return nil
	})
	cron.Remove(id)

	select {
	case <-time.After(OneSecond):
	case <-wait(wg):
		t.FailNow()
	}
}

// Test timing with Entries.
func TestSnapshotEntries(t *testing.T) {
	wg := &sync.WaitGroup{}
	wg.Add(1)

	cron := New()
	cron.AddFunc("TestSnapshotEntries", "@every 2s", func(context.Context) error {
		wg.Done()
		return nil
	})
	cron.Start(context.TODO())
	defer cron.Stop(context.TODO())

	// Cron should fire in 2 seconds. After 1 second, call Entries.

	<-time.After(OneSecond)
	cron.Entries()

	// Even though Entries was called, the cron should fire at the 2 second mark.
	select {
	case <-time.After(OneSecond):
		t.Error("expected job runs at 2 second mark")
	case <-wait(wg):
	}
}

// Test that the entries are correctly sorted.
// Add a bunch of long-in-the-future entries, and an immediate entry, and ensure
// that the immediate entry runs immediately.
// Also: Test that multiple jobs run in the same instant.
func TestMultipleEntries(t *testing.T) {
	wg := &sync.WaitGroup{}
	wg.Add(2)

	cron := newWithSeconds()
	cron.AddFunc("TestMultipleEntries", "0 0 0 1 1 ?", func(context.Context) error {
		return nil
	})
	cron.AddFunc("TestMultipleEntries", "* * * * * ?", func(context.Context) error {
		wg.Done()
		return nil
	})
	id1, _ := cron.AddFunc("TestMultipleEntries", "* * * * * ?", func(context.Context) error {
		t.Fatal()
		return nil
	})
	id2, _ := cron.AddFunc("TestMultipleEntries", "* * * * * ?", func(context.Context) error {
		t.Fatal()
		return nil
	})
	cron.AddFunc("TestMultipleEntries", "0 0 0 31 12 ?", func(context.Context) error {
		return nil
	})
	cron.AddFunc("TestMultipleEntries", "* * * * * ?", func(context.Context) error {
		wg.Done()
		return nil
	})

	cron.Remove(id1)
	cron.Start(context.TODO())
	cron.Remove(id2)
	defer cron.Stop(context.TODO())

	select {
	case <-time.After(OneSecond):
		t.Error("expected job run in proper order")
	case <-wait(wg):
	}
}

// Test running the same job twice.
func TestRunningJobTwice(t *testing.T) {
	wg := &sync.WaitGroup{}
	wg.Add(2)

	cron := newWithSeconds()
	cron.AddFunc("TestRunningJobTwice", "0 0 0 1 1 ?", func(context.Context) error {
		return nil
	})
	cron.AddFunc("TestRunningJobTwice", "0 0 0 31 12 ?", func(context.Context) error {
		return nil
	})
	cron.AddFunc("TestRunningJobTwice", "* * * * * ?", func(context.Context) error {
		wg.Done()
		return nil
	})

	cron.Start(context.TODO())
	defer cron.Stop(context.TODO())

	select {
	case <-time.After(2 * OneSecond):
		t.Error("expected job fires 2 times")
	case <-wait(wg):
	}
}

func TestRunningMultipleSchedules(t *testing.T) {
	wg := &sync.WaitGroup{}
	wg.Add(2)

	cron := newWithSeconds()
	cron.AddFunc("TestRunningMultipleSchedules", "0 0 0 1 1 ?", func(context.Context) error {
		return nil
	})
	cron.AddFunc("TestRunningMultipleSchedules", "0 0 0 31 12 ?", func(context.Context) error {
		return nil
	})
	cron.AddFunc("TestRunningMultipleSchedules", "* * * * * ?", func(context.Context) error {
		wg.Done()
		return nil
	})
	cron.Schedule("TestRunningMultipleSchedules", Every(time.Minute), FuncJob(func(context.Context) error {
		return nil
	}))
	cron.Schedule("TestRunningMultipleSchedules", Every(time.Second), FuncJob(func(context.Context) error {
		wg.Done()
		return nil
	}))
	cron.Schedule("TestRunningMultipleSchedules", Every(time.Hour), FuncJob(func(context.Context) error {
		return nil
	}))

	cron.Start(context.TODO())
	defer cron.Stop(context.TODO())

	select {
	case <-time.After(2 * OneSecond):
		t.Error("expected job fires 2 times")
	case <-wait(wg):
	}
}

// Test that the cron is run in the local time zone (as opposed to UTC).
func TestLocalTimezone(t *testing.T) {
	wg := &sync.WaitGroup{}
	wg.Add(2)

	now := time.Now()
	// FIX: Issue #205
	// This calculation doesn't work in seconds 58 or 59.
	// Take the easy way out and sleep.
	if now.Second() >= 58 {
		time.Sleep(2 * time.Second)
		now = time.Now()
	}
	spec := fmt.Sprintf("%d,%d %d %d %d %d ?",
		now.Second()+1, now.Second()+2, now.Minute(), now.Hour(), now.Day(), now.Month())

	cron := newWithSeconds()
	cron.AddFunc("TestLocalTimezone", spec, func(context.Context) error {
		wg.Done()
		return nil
	})
	cron.Start(context.TODO())
	defer cron.Stop(context.TODO())

	select {
	case <-time.After(OneSecond * 2):
		t.Error("expected job fires 2 times")
	case <-wait(wg):
	}
}

// Test that the cron is run in the given time zone (as opposed to local).
func TestNonLocalTimezone(t *testing.T) {
	wg := &sync.WaitGroup{}
	wg.Add(2)

	loc, err := time.LoadLocation("Atlantic/Cape_Verde")
	if err != nil {
		fmt.Printf("Failed to load time zone Atlantic/Cape_Verde: %+v", err)
		t.Fail()
	}

	now := time.Now().In(loc)
	// FIX: Issue #205
	// This calculation doesn't work in seconds 58 or 59.
	// Take the easy way out and sleep.
	if now.Second() >= 58 {
		time.Sleep(2 * time.Second)
		now = time.Now().In(loc)
	}
	spec := fmt.Sprintf("%d,%d %d %d %d %d ?",
		now.Second()+1, now.Second()+2, now.Minute(), now.Hour(), now.Day(), now.Month())

	cron := New(WithLocation(loc), WithParser(secondParser))
	cron.AddFunc("TestNonLocalTimezone", spec, func(context.Context) error {
		wg.Done()
		return nil
	})
	cron.Start(context.TODO())
	defer cron.Stop(context.TODO())

	select {
	case <-time.After(OneSecond * 2):
		t.Error("expected job fires 2 times")
	case <-wait(wg):
	}
}

// Test that calling stop before start silently returns without
// blocking the stop channel.
func TestStopWithoutStart(t *testing.T) {
	cron := New()
	cron.Stop(context.TODO())
}

type testJob struct {
	wg   *sync.WaitGroup
	name string
}

func (t testJob) Run(context.Context) error {
	t.wg.Done()
	return nil
}

// Test that adding an invalid job spec returns an error
func TestInvalidJobSpec(t *testing.T) {
	cron := New()
	_, err := cron.AddJob("TestInvalidJobSpec", "this will not parse", nil)
	if err == nil {
		t.Errorf("expected an error with invalid spec, got nil")
	}
}

// Test blocking run method behaves as Start()
func TestBlockingRun(t *testing.T) {
	wg := &sync.WaitGroup{}
	wg.Add(1)

	cron := newWithSeconds()
	cron.AddFunc("TestBlockingRun", "* * * * * ?", func(context.Context) error {

		wg.Done()
		return nil
	})

	var unblockChan = make(chan struct{})

	go func() {
		cron.Run(context.TODO())
		close(unblockChan)
	}()
	defer cron.Stop(context.TODO())

	select {
	case <-time.After(OneSecond):
		t.Error("expected job fires")
	case <-unblockChan:
		t.Error("expected that Run() blocks")
	case <-wait(wg):
	}
}

// Test that double-running is a no-op
func TestStartNoop(t *testing.T) {
	var tickChan = make(chan struct{}, 2)

	cron := newWithSeconds()
	cron.AddFunc("TestInvalidJobSpec", "* * * * * ?", func(context.Context) error {
		tickChan <- struct{}{}
		return nil
	})

	cron.Start(context.TODO())
	defer cron.Stop(context.TODO())

	// Wait for the first firing to ensure the runner is going
	<-tickChan

	cron.Start(context.TODO())

	<-tickChan

	// Fail if this job fires again in a short period, indicating a double-run
	select {
	case <-time.After(time.Millisecond):
	case <-tickChan:
		t.Error("expected job fires exactly twice")
	}
}

// Simple test using Runnables.
func TestJob(t *testing.T) {
	wg := &sync.WaitGroup{}
	wg.Add(1)

	cron := newWithSeconds()
	cron.AddJob("TestJob", "0 0 0 30 Feb ?", testJob{wg, "job0"})
	cron.AddJob("TestJob", "0 0 0 1 1 ?", testJob{wg, "job1"})
	job2, _ := cron.AddJob("TestJob", "* * * * * ?", testJob{wg, "job2"})
	cron.AddJob("TestJob", "1 0 0 1 1 ?", testJob{wg, "job3"})
	cron.Schedule("TestJob", Every(5*time.Second+5*time.Nanosecond), testJob{wg, "job4"})
	job5 := cron.Schedule("TestJob", Every(5*time.Minute), testJob{wg, "job5"})

	// Test getting an Entry pre-Start.
	if actualName := cron.Entry(job2).Job.(testJob).name; actualName != "job2" {
		t.Error("wrong job retrieved:", actualName)
	}
	if actualName := cron.Entry(job5).Job.(testJob).name; actualName != "job5" {
		t.Error("wrong job retrieved:", actualName)
	}

	cron.Start(context.TODO())
	defer cron.Stop(context.TODO())

	select {
	case <-time.After(OneSecond):
		t.FailNow()
	case <-wait(wg):
	}

	// Ensure the entries are in the right order.
	expecteds := []string{"job2", "job4", "job5", "job1", "job3", "job0"}

	var actuals []string
	for _, entry := range cron.Entries() {
		actuals = append(actuals, entry.Job.(testJob).name)
	}

	for i, expected := range expecteds {
		if actuals[i] != expected {
			t.Fatalf("Jobs not in the right order.  (expected) %s != %s (actual)", expecteds, actuals)
		}
	}

	// Test getting Entries.
	if actualName := cron.Entry(job2).Job.(testJob).name; actualName != "job2" {
		t.Error("wrong job retrieved:", actualName)
	}
	if actualName := cron.Entry(job5).Job.(testJob).name; actualName != "job5" {
		t.Error("wrong job retrieved:", actualName)
	}
}

// Issue #206
// Ensure that the next run of a job after removing an entry is accurate.
func TestScheduleAfterRemoval(t *testing.T) {
	var wg1 sync.WaitGroup
	var wg2 sync.WaitGroup
	wg1.Add(1)
	wg2.Add(1)

	// The first time this job is run, set a timer and remove the other job
	// 750ms later. Correct behavior would be to still run the job again in
	// 250ms, but the bug would cause it to run instead 1s later.

	var calls int
	var mu sync.Mutex

	cron := newWithSeconds()
	hourJob := cron.Schedule("TestScheduleAfterRemoval", Every(time.Hour), FuncJob(func(context.Context) error { return nil }))
	cron.Schedule("TestScheduleAfterRemoval", Every(time.Second), FuncJob(func(context.Context) error {
		mu.Lock()
		defer mu.Unlock()
		switch calls {
		case 0:
			wg1.Done()
			calls++
		case 1:
			time.Sleep(750 * time.Millisecond)
			cron.Remove(hourJob)
			calls++
		case 2:
			calls++
			wg2.Done()
		case 3:
			panic("unexpected 3rd call")
		}
		return nil
	}))

	cron.Start(context.TODO())
	defer cron.Stop(context.TODO())

	// the first run might be any length of time 0 - 1s, since the schedule
	// rounds to the second. wait for the first run to true up.
	wg1.Wait()

	select {
	case <-time.After(2 * OneSecond):
		t.Error("expected job fires 2 times")
	case <-wait(&wg2):
	}
}

type ZeroSchedule struct{}

func (*ZeroSchedule) Next(time.Time) time.Time {
	return time.Time{}
}

// Tests that job without time does not run
func TestJobWithZeroTimeDoesNotRun(t *testing.T) {
	cron := newWithSeconds()
	var calls int64
	cron.AddFunc("TestJobWithZeroTimeDoesNotRun", "* * * * * *", func(context.Context) error { atomic.AddInt64(&calls, 1); return nil })
	cron.Schedule("TestJobWithZeroTimeDoesNotRun", new(ZeroSchedule), FuncJob(func(context.Context) error { t.Error("expected zero task will not run"); return nil }))
	cron.Start(context.TODO())
	defer cron.Stop(context.TODO())
	<-time.After(OneSecond)
	if atomic.LoadInt64(&calls) != 1 {
		t.Errorf("called %d times, expected 1\n", calls)
	}
}

// func TestStopAndWait(t *testing.T) {
// 	t.Run("nothing running, returns immediately", func(t *testing.T) {
// 		cron := newWithSeconds()
// 		cron.Start(context.TODO())
// 		ctx := cron.Stop(context.TODO())
// 		select {
// 		case <-ctx.Done():
// 		case <-time.After(time.Millisecond):
// 			t.Error("context was not done immediately")
// 		}
// 	})

// 	t.Run("repeated calls to Stop", func(t *testing.T) {
// 		cron := newWithSeconds()
// 		cron.Start(context.TODO())
// 		_ = cron.Stop(context.TODO())
// 		time.Sleep(time.Millisecond)
// 		ctx := cron.Stop(context.TODO())
// 		select {
// 		case <-ctx.Done():
// 		case <-time.After(time.Millisecond):
// 			t.Error("context was not done immediately")
// 		}
// 	})

// 	t.Run("a couple fast jobs added, still returns immediately", func(t *testing.T) {
// 		cron := newWithSeconds()
// 		cron.AddFunc("TestStopAndWait", "* * * * * *", func(context.Context) error { return nil })
// 		cron.Start(context.TODO())
// 		cron.AddFunc("TestStopAndWait", "* * * * * *", func(context.Context) error { return nil })
// 		cron.AddFunc("TestStopAndWait", "* * * * * *", func(context.Context) error { return nil })
// 		cron.AddFunc("TestStopAndWait", "* * * * * *", func(context.Context) error { return nil })
// 		time.Sleep(time.Second)
// 		ctx := cron.Stop(context.TODO())
// 		select {
// 		case <-ctx.Done():
// 		case <-time.After(time.Millisecond):
// 			t.Error("context was not done immediately")
// 		}
// 	})

// 	t.Run("a couple fast jobs and a slow job added, waits for slow job", func(t *testing.T) {
// 		cron := newWithSeconds()
// 		cron.AddFunc("TestStopAndWait", "* * * * * *", func(context.Context) error { return nil })
// 		cron.Start(context.TODO())
// 		cron.AddFunc("TestStopAndWait", "* * * * * *", func(context.Context) error { time.Sleep(2 * time.Second); return nil })
// 		cron.AddFunc("TestStopAndWait", "* * * * * *", func(context.Context) error { return nil })
// 		time.Sleep(time.Second)

// 		ctx := cron.Stop(context.TODO())

// 		// Verify that it is not done for at least 750ms
// 		select {
// 		case <-ctx.Done():
// 			t.Error("context was done too quickly immediately")
// 		case <-time.After(750 * time.Millisecond):
// 			// expected, because the job sleeping for 1 second is still running
// 		}

// 		// Verify that it IS done in the next 500ms (giving 250ms buffer)
// 		select {
// 		case <-ctx.Done():
// 			// expected
// 		case <-time.After(1500 * time.Millisecond):
// 			t.Error("context not done after job should have completed")
// 		}
// 	})

// 	t.Run("repeated calls to stop, waiting for completion and after", func(t *testing.T) {
// 		cron := newWithSeconds()
// 		cron.AddFunc("TestStopAndWait", "* * * * * *", func(context.Context) error { return nil })
// 		cron.AddFunc("TestStopAndWait", "* * * * * *", func(context.Context) error { time.Sleep(2 * time.Second); return nil })
// 		cron.Start(context.TODO())
// 		cron.AddFunc("TestStopAndWait", "* * * * * *", func(context.Context) error { return nil })
// 		time.Sleep(time.Second)
// 		ctx := cron.Stop(context.TODO())
// 		ctx2 := cron.Stop(context.TODO())

// 		// Verify that it is not done for at least 1500ms
// 		select {
// 		case <-ctx.Done():
// 			t.Error("context was done too quickly immediately")
// 		case <-ctx2.Done():
// 			t.Error("context2 was done too quickly immediately")
// 		case <-time.After(1500 * time.Millisecond):
// 			// expected, because the job sleeping for 2 seconds is still running
// 		}

// 		// Verify that it IS done in the next 1s (giving 500ms buffer)
// 		select {
// 		case <-ctx.Done():
// 			// expected
// 		case <-time.After(time.Second):
// 			t.Error("context not done after job should have completed")
// 		}

// 		// Verify that ctx2 is also done.
// 		select {
// 		case <-ctx2.Done():
// 			// expected
// 		case <-time.After(time.Millisecond):
// 			t.Error("context2 not done even though context1 is")
// 		}

// 		// Verify that a new context retrieved from stop is immediately done.
// 		ctx3 := cron.Stop(context.TODO())
// 		select {
// 		case <-ctx3.Done():
// 			// expected
// 		case <-time.After(time.Millisecond):
// 			t.Error("context not done even when cron Stop is completed")
// 		}

// 	})
// }

func TestMultiThreadedStartAndStop(t *testing.T) {
	cron := New()
	go cron.Run(context.TODO())
	time.Sleep(2 * time.Millisecond)
	cron.Stop(context.TODO())
}

func wait(wg *sync.WaitGroup) chan bool {
	ch := make(chan bool)
	go func() {
		wg.Wait()
		ch <- true
	}()
	return ch
}

func stop(cron *Cron) chan bool {
	ch := make(chan bool)
	go func() {
		cron.Stop(context.TODO())
		ch <- true
	}()
	return ch
}

// newWithSeconds returns a Cron with the seconds field enabled.
func newWithSeconds() *Cron {
	return New(WithParser(secondParser), WithChain())
}