package pipeline

import (
	"context"
	"fmt"
)

type parallel struct {
	id    string
	tasks []Task
}

// Parallel returns a Stage that passes a copy of each incoming Data
// to all specified tasks, waits for all the tasks to finish before
// sending data to the next stage, and only passes the original Data
// through to the following stage.
func Parallel(id string, tasks ...Task) Stage {
	if len(tasks) == 0 {
		return nil
	}

	return &parallel{
		id:    id,
		tasks: tasks,
	}
}

// ID implements Stage.
func (p *parallel) ID() string {
	return p.id
}

// Run implements Stage.
func (p *parallel) Run(ctx context.Context, sp StageParams) {
	for {
		select {
		case <-ctx.Done():
			return
		case dataIn, ok := <-sp.Input():
			if !ok {
				return
			}
			p.executeTask(ctx, dataIn, sp)
		case <-sp.DataQueue().Signal():
			if d, ok := sp.DataQueue().Next(); ok {
				if data, ok := d.(Data); ok {
					p.executeTask(ctx, data, sp)
				}
			}
		}
	}
}

func (p *parallel) executeTask(ctx context.Context, data Data, sp StageParams) {
	done := make(chan Data, len(p.tasks))

	for i := 0; i < len(p.tasks); i++ {
		c := data.Clone()

		select {
		case <-ctx.Done():
			return
		case sp.NewData() <- c:
		}

		go func(idx int, clone Data) {
			tp := &taskParams{
				newdata:   sp.NewData(),
				processed: sp.ProcessedData(),
				registry:  sp.Registry(),
			}

			d, err := p.tasks[idx].Process(ctx, clone, tp)
			if err != nil {
				sp.Error().Append(fmt.Errorf("pipeline stage %d: %v", sp.Position(), err))
			}

			sp.ProcessedData() <- clone
			clone.MarkAsProcessed()
			done <- d
		}(i, c)
	}

	var failed bool
	for i := 0; i < len(p.tasks); i++ {
		if d := <-done; d == nil {
			failed = true
		}
	}
	if failed {
		sp.ProcessedData() <- data
		data.MarkAsProcessed()
		return
	}

	select {
	case <-ctx.Done():
	case sp.Output() <- data:
	}
}
