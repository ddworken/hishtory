package shared

import "sync"

func ForEach[T any](arr []T, numThreads int, fn func(T) error) (err error) {
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
