package parallel

import (
	"context"
	"runtime"
	"sync"
)

const maxWorkerCount = 8

type taskIndexed[T any] struct {
	index int
	value T
}

type resultIndexed[R any] struct {
	index int
	value R
	err   error
}

func MapOrdered[T any, R any](values []T, fnMap func(T) (R, error)) ([]R, error) {
	if len(values) == 0 {
		return []R{}, nil
	}

	ctx, fnCancel := context.WithCancel(context.Background())
	defer fnCancel()

	channelTasks := make(chan taskIndexed[T])
	channelResults := make(chan resultIndexed[R], len(values))

	countWorkers := deriveWorkerCountMax(len(values), 0)
	var groupWorkers sync.WaitGroup
	groupWorkers.Add(countWorkers)
	for range countWorkers {
		go func() {
			defer groupWorkers.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case itemTask, ok := <-channelTasks:
					if !ok {
						return
					}
					valueMapped, err := fnMap(itemTask.value)
					channelResults <- resultIndexed[R]{
						index: itemTask.index,
						value: valueMapped,
						err:   err,
					}
					if err != nil {
						fnCancel()
						return
					}
				}
			}
		}()
	}

	go func() {
		defer close(channelTasks)
		for index, value := range values {
			select {
			case <-ctx.Done():
				return
			case channelTasks <- taskIndexed[T]{index: index, value: value}:
			}
		}
	}()

	go func() {
		groupWorkers.Wait()
		close(channelResults)
	}()

	valuesMapped := make([]R, len(values))
	var errFirst error
	for itemResult := range channelResults {
		if itemResult.err != nil {
			if errFirst == nil {
				errFirst = itemResult.err
			}
			continue
		}
		valuesMapped[itemResult.index] = itemResult.value
	}
	if errFirst != nil {
		return nil, errFirst
	}
	return valuesMapped, nil
}

func MapOrderedWithWorkers[T any, R any](
	values []T,
	workersMax int,
	fnMap func(T) (R, error),
) ([]R, error) {
	if len(values) == 0 {
		return []R{}, nil
	}

	ctx, fnCancel := context.WithCancel(context.Background())
	defer fnCancel()

	channelTasks := make(chan taskIndexed[T])
	channelResults := make(chan resultIndexed[R], len(values))

	countWorkers := deriveWorkerCountMax(len(values), workersMax)
	var groupWorkers sync.WaitGroup
	groupWorkers.Add(countWorkers)
	for range countWorkers {
		go func() {
			defer groupWorkers.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case itemTask, ok := <-channelTasks:
					if !ok {
						return
					}
					valueMapped, err := fnMap(itemTask.value)
					channelResults <- resultIndexed[R]{
						index: itemTask.index,
						value: valueMapped,
						err:   err,
					}
					if err != nil {
						fnCancel()
						return
					}
				}
			}
		}()
	}

	go func() {
		defer close(channelTasks)
		for index, value := range values {
			select {
			case <-ctx.Done():
				return
			case channelTasks <- taskIndexed[T]{index: index, value: value}:
			}
		}
	}()

	go func() {
		groupWorkers.Wait()
		close(channelResults)
	}()

	valuesMapped := make([]R, len(values))
	var errFirst error
	for itemResult := range channelResults {
		if itemResult.err != nil {
			if errFirst == nil {
				errFirst = itemResult.err
			}
			continue
		}
		valuesMapped[itemResult.index] = itemResult.value
	}
	if errFirst != nil {
		return nil, errFirst
	}
	return valuesMapped, nil
}

func deriveWorkerCount(countTasks int) int {
	return deriveWorkerCountMax(countTasks, 0)
}

func deriveWorkerCountMax(countTasks int, workersMax int) int {
	if countTasks <= 0 {
		return 1
	}
	countWorkers := workersMax
	if countWorkers <= 0 {
		countWorkers = runtime.NumCPU()
	}
	if countWorkers < 1 {
		countWorkers = 1
	}
	if countWorkers > maxWorkerCount {
		countWorkers = maxWorkerCount
	}
	if countWorkers > countTasks {
		countWorkers = countTasks
	}
	return countWorkers
}
