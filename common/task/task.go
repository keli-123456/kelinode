package task

import (
	"context"
	"errors"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

type Task struct {
	Name      string
	Interval  time.Duration
	Timeout   time.Duration
	Execute   func(ctx context.Context) error
	OnTimeout func()
	Access    sync.RWMutex
	Running   bool
	Stop      chan struct{}
}

func (t *Task) Start(first bool) error {
	t.Access.Lock()
	if t.Running {
		t.Access.Unlock()
		return nil
	}
	t.Running = true
	t.Stop = make(chan struct{})
	t.Access.Unlock()

	go func() {
		defer t.safeStop()

		timer := time.NewTimer(t.Interval)
		defer timer.Stop()
		if first {
			if err := t.ExecuteWithTimeout(); err != nil {
				log.Errorf("Task %s execution error: %v", t.Name, err)
				return
			}
		}

		for {
			timer.Reset(t.Interval)
			select {
			case <-timer.C:
				// continue
			case <-t.Stop:
				return
			}

			if err := t.ExecuteWithTimeout(); err != nil {
				log.Errorf("Task %s execution error: %v", t.Name, err)
				return
			}
		}
	}()

	return nil
}

func (t *Task) ExecuteWithTimeout() error {
	timeout := t.Timeout
	if timeout <= 0 {
		timeout = min(3*t.Interval, 5*time.Minute)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	err := t.Execute(ctx)
	if err == nil {
		return nil
	}

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		log.WithField("timeout", timeout).Errorf("Task %s execution timed out", t.Name)
		if t.OnTimeout != nil {
			t.OnTimeout()
		}
		return nil
	}

	return err
}

func (t *Task) safeStop() {
	t.Access.Lock()
	if t.Running {
		t.Running = false
		close(t.Stop)
	}
	t.Access.Unlock()
}

func (t *Task) Close() {
	t.safeStop()
	log.Infof("Task %s stopped", t.Name)
}
