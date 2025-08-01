// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package queue

import (
	"container/heap"
	"sync"

	"github.com/luxfi/ids"
)

// Job represents a job to be executed
type Job interface {
	ID() ids.ID
	Priority() uint64
	Execute() error
}

// Queue is a priority queue for jobs
type Queue interface {
	// Push adds a job to the queue
	Push(Job)

	// Pop removes and returns the highest priority job
	Pop() (Job, bool)

	// Len returns the number of jobs in the queue
	Len() int

	// Has returns true if the job is in the queue
	Has(ids.ID) bool

	// Remove removes a job from the queue
	Remove(ids.ID) bool
}

type queue struct {
	lock sync.RWMutex
	heap priorityHeap
	jobs map[ids.ID]int // jobID -> index in heap
}

// NewQueue returns a new job queue
func NewQueue() Queue {
	return &queue{
		jobs: make(map[ids.ID]int),
	}
}

func (q *queue) Push(job Job) {
	q.lock.Lock()
	defer q.lock.Unlock()

	id := job.ID()
	if _, exists := q.jobs[id]; exists {
		return
	}

	heap.Push(&q.heap, job)
	q.jobs[id] = len(q.heap) - 1
}

func (q *queue) Pop() (Job, bool) {
	q.lock.Lock()
	defer q.lock.Unlock()

	if len(q.heap) == 0 {
		return nil, false
	}

	job := heap.Pop(&q.heap).(Job)
	delete(q.jobs, job.ID())
	
	// Update indices
	for i, j := range q.heap {
		q.jobs[j.ID()] = i
	}
	
	return job, true
}

func (q *queue) Len() int {
	q.lock.RLock()
	defer q.lock.RUnlock()
	return len(q.heap)
}

func (q *queue) Has(id ids.ID) bool {
	q.lock.RLock()
	defer q.lock.RUnlock()
	_, exists := q.jobs[id]
	return exists
}

func (q *queue) Remove(id ids.ID) bool {
	q.lock.Lock()
	defer q.lock.Unlock()

	index, exists := q.jobs[id]
	if !exists {
		return false
	}

	heap.Remove(&q.heap, index)
	delete(q.jobs, id)
	
	// Update indices
	for i, j := range q.heap {
		q.jobs[j.ID()] = i
	}
	
	return true
}

// priorityHeap implements heap.Interface
type priorityHeap []Job

func (h priorityHeap) Len() int           { return len(h) }
func (h priorityHeap) Less(i, j int) bool { return h[i].Priority() > h[j].Priority() }
func (h priorityHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *priorityHeap) Push(x interface{}) {
	*h = append(*h, x.(Job))
}

func (h *priorityHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

// Jobs tracks jobs that can be executed
type Jobs struct {
	queue Queue
}

// NewJobs returns a new Jobs instance
func NewJobs() *Jobs {
	return &Jobs{
		queue: NewQueue(),
	}
}

// Push adds a job
func (j *Jobs) Push(job Job) {
	j.queue.Push(job)
}

// Pop removes and returns the highest priority job
func (j *Jobs) Pop() (Job, bool) {
	return j.queue.Pop()
}

// Len returns the number of jobs
func (j *Jobs) Len() int {
	return j.queue.Len()
}

// JobsWithMissing tracks jobs that are blocked on missing items
type JobsWithMissing struct {
	jobs    *Jobs
	missing map[ids.ID][]Job // missing ID -> jobs waiting for it
	mu      sync.RWMutex
}

// NewJobsWithMissing returns a new JobsWithMissing instance
func NewJobsWithMissing() *JobsWithMissing {
	return &JobsWithMissing{
		jobs:    NewJobs(),
		missing: make(map[ids.ID][]Job),
	}
}

// AddMissingID adds a job that's waiting for a missing ID
func (j *JobsWithMissing) AddMissingID(missingID ids.ID, job Job) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.missing[missingID] = append(j.missing[missingID], job)
}

// RemoveMissingID removes jobs waiting for an ID and returns them
func (j *JobsWithMissing) RemoveMissingID(id ids.ID) []Job {
	j.mu.Lock()
	defer j.mu.Unlock()
	jobs := j.missing[id]
	delete(j.missing, id)
	return jobs
}

// HasMissingIDs returns true if there are missing IDs
func (j *JobsWithMissing) HasMissingIDs() bool {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return len(j.missing) > 0
}

// MissingIDs returns the IDs that are missing
func (j *JobsWithMissing) MissingIDs() []ids.ID {
	j.mu.RLock()
	defer j.mu.RUnlock()
	ids := make([]ids.ID, 0, len(j.missing))
	for id := range j.missing {
		ids = append(ids, id)
	}
	return ids
}