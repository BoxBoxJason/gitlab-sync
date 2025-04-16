package utils

import "sync"

// concurrencyManagerImpl manages the concurrency limit for goroutines.
type concurrencyManagerImpl struct {
	sem chan struct{}
	mu  sync.Mutex
}

// concurrencyManagerAPI defines the interface for the global ConcurrencyManager.
type concurrencyManagerAPI interface {
	Acquire()
	Release()
	SetLimit(limit int)
	Wait(wg *sync.WaitGroup)
}

// ConcurrencyManager is the global instance of the concurrency manager.
var ConcurrencyManager concurrencyManagerAPI = &concurrencyManagerImpl{
	sem: make(chan struct{}, 1), // Default limit of 1 until explicitly set
}

// Acquire blocks until a slot is available.
func (cm *concurrencyManagerImpl) Acquire() {
	cm.sem <- struct{}{}
}

// Release releases a slot.
func (cm *concurrencyManagerImpl) Release() {
	<-cm.sem
}

// SetLimit dynamically adjusts the concurrency limit.
func (cm *concurrencyManagerImpl) SetLimit(limit int) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Resize the semaphore to match the new limit
	newSem := make(chan struct{}, limit)
	// Transfer existing tokens to the new semaphore
	for i := 0; i < len(cm.sem) && len(newSem) < cap(newSem); i++ {
		newSem <- struct{}{}
	}
	cm.sem = newSem
}

// Wait ensures all goroutines finish by waiting for the WaitGroup and closing the semaphore.
func (cm *concurrencyManagerImpl) Wait(wg *sync.WaitGroup) {
	wg.Wait()
	close(cm.sem)
}

// ExecuteWithConcurrency manages concurrency for API calls.
func ExecuteWithConcurrency(apiFunc func() error, wg *sync.WaitGroup, errorChan chan<- error) {
	defer wg.Done()

	// Acquire a concurrency slot
	ConcurrencyManager.Acquire()
	defer ConcurrencyManager.Release()

	// Execute the API function
	if err := apiFunc(); err != nil {
		errorChan <- err
	}
}
