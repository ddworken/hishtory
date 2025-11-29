package shared

import "sync"

func ForEach[T any](arr []T, numThreads int, fn func(T) error) error {
	wg := &sync.WaitGroup{}
	wg.Add(len(arr))

	limiter := make(chan bool, numThreads)

	var errors []error
	for _, item := range arr {
		limiter <- true
		go func(x T) {
			defer wg.Done()
			err := fn(x)
			if err != nil {
				errors = append(errors, err)
			}
			<-limiter
		}(item)
		if len(errors) > 0 {
			return errors[0]
		}
	}

	wg.Wait()
	if len(errors) > 0 {
		return errors[0]
	}
	return nil
}

type mapResult[R any] struct {
	result R
	err    error
	index  int
}

// ParallelMap applies fn to each element of arr in parallel and returns the results
func ParallelMap[T, R any](arr []T, fn func(T) (R, error)) ([]R, error) {
	results := make([]R, len(arr))
	resultsChan := make(chan mapResult[R], len(arr))
	wg := &sync.WaitGroup{}

	// Launch parallel operations
	for i, item := range arr {
		wg.Add(1)
		go func(index int, x T) {
			defer wg.Done()
			result, err := fn(x)
			resultsChan <- mapResult[R]{result: result, err: err, index: index}
		}(i, item)
	}

	// Wait for all goroutines to complete and close channel
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect results
	for result := range resultsChan {
		if result.err != nil {
			return nil, result.err
		}
		results[result.index] = result.result
	}

	return results, nil
}
