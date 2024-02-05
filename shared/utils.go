package shared

import "sync"

func ForEach[T any](iter Seq1[T], numThreads int, fn func(T) error) error {
	wg := &sync.WaitGroup{}
	limiter := make(chan bool, numThreads)
	var errors []error
	iter(func(item T) bool {
		wg.Add(1)
		limiter <- true
		go func(x T) {
			defer wg.Done()
			err := fn(x)
			if err != nil {
				errors = append(errors, err)
			}
			<-limiter
		}(item)
		return true
	})
	wg.Wait()
	if len(errors) > 0 {
		return errors[0]
	}
	return nil
}
