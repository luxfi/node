// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package aivm

import (
	"context"
	"errors"
	"sync"
	"time"

	"go.uber.org/zap"
	
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/utils/logging"
)

// GPUSubnet represents a GPU-powered subnet for AI computation
type GPUSubnet struct {
	SubnetID     ids.ID          `json:"subnetId"`
	Name         string          `json:"name"`
	Config       GPUSubnetConfig `json:"config"`
	Providers    []GPUProvider   `json:"providers"`
	Tasks        []GPUTask       `json:"tasks"`
	Performance  SubnetMetrics   `json:"performance"`
	mu           sync.RWMutex
	log          logging.Logger
}

// GPUSubnetConfig defines configuration for GPU subnet
type GPUSubnetConfig struct {
	MinGPUCount      int                `json:"minGpuCount"`
	RequiredGPUTypes []string           `json:"requiredGpuTypes"`
	TaskTypes        []string           `json:"taskTypes"`
	PricingModel     PricingModel       `json:"pricingModel"`
	QualityMetrics   QualityRequirement `json:"qualityMetrics"`
	NetworkTopology  string             `json:"networkTopology"` // "mesh", "star", "hybrid"
}

// GPUProvider represents a GPU compute provider
type GPUProvider struct {
	NodeID       ids.NodeID     `json:"nodeId"`
	ProviderInfo ProviderInfo   `json:"providerInfo"`
	GPUs         []GPUSpec      `json:"gpus"`
	Status       ProviderStatus `json:"status"`
	Reputation   float64        `json:"reputation"`
	TasksHandled uint64         `json:"tasksHandled"`
	JoinedAt     time.Time      `json:"joinedAt"`
}

// ProviderInfo contains provider details
type ProviderInfo struct {
	Name         string   `json:"name"`
	Location     string   `json:"location"`
	Capabilities []string `json:"capabilities"`
	Pricing      Pricing  `json:"pricing"`
}

// GPUSpec defines GPU specifications
type GPUSpec struct {
	Model       string  `json:"model"`       // e.g., "RTX 4090", "A100", "H100"
	Memory      uint64  `json:"memory"`      // VRAM in GB
	ComputeUnits int    `json:"computeUnits"`
	Performance float64 `json:"performance"` // TFLOPS
	Available   bool    `json:"available"`
	Temperature float64 `json:"temperature"`
	Utilization float64 `json:"utilization"`
}

// GPUTask represents an AI computation task
type GPUTask struct {
	TaskID       ids.ID         `json:"taskId"`
	Type         string         `json:"type"` // "training", "inference", "rendering", "mining"
	Requirements TaskRequirements `json:"requirements"`
	Requester    ids.ShortID    `json:"requester"`
	Provider     ids.NodeID     `json:"provider"`
	Status       TaskStatus     `json:"status"`
	Result       GPUTaskResult     `json:"result"`
	CreatedAt    time.Time      `json:"createdAt"`
	StartedAt    time.Time      `json:"startedAt"`
	CompletedAt  time.Time      `json:"completedAt"`
}

// TaskRequirements defines task requirements
type TaskRequirements struct {
	GPUModel     string            `json:"gpuModel,omitempty"`
	MinMemory    uint64            `json:"minMemory"`
	MinCompute   float64           `json:"minCompute"`
	MaxLatency   time.Duration     `json:"maxLatency"`
	DataSize     uint64            `json:"dataSize"`
	ModelSize    uint64            `json:"modelSize,omitempty"`
	Framework    string            `json:"framework,omitempty"` // "pytorch", "tensorflow", "jax"
	CustomParams map[string]string `json:"customParams,omitempty"`
}

// GPUTaskResult contains GPU task execution results
type GPUTaskResult struct {
	Success      bool              `json:"success"`
	Output       []byte            `json:"output,omitempty"`
	Metrics      ComputeMetrics    `json:"metrics"`
	ProofOfWork  []byte            `json:"proofOfWork"`
	Error        string            `json:"error,omitempty"`
}

// ComputeMetrics tracks computation metrics
type ComputeMetrics struct {
	ExecutionTime time.Duration `json:"executionTime"`
	GPUTime       time.Duration `json:"gpuTime"`
	MemoryUsed    uint64        `json:"memoryUsed"`
	PowerUsed     float64       `json:"powerUsed"` // Watts
	Accuracy      float64       `json:"accuracy,omitempty"`
	Throughput    float64       `json:"throughput,omitempty"`
}

// SubnetMetrics tracks subnet performance
type SubnetMetrics struct {
	TotalTasks       uint64        `json:"totalTasks"`
	CompletedTasks   uint64        `json:"completedTasks"`
	FailedTasks      uint64        `json:"failedTasks"`
	AverageLatency   time.Duration `json:"averageLatency"`
	TotalGPUHours    float64       `json:"totalGpuHours"`
	NetworkHashrate  float64       `json:"networkHashrate"`
	ActiveProviders  int           `json:"activeProviders"`
}

// PricingModel defines pricing for GPU compute
type PricingModel struct {
	BasePrice       uint64            `json:"basePrice"`      // Per GPU hour
	PricePerTFLOPS  uint64            `json:"pricePerTflops"`
	SurgeMultiplier float64           `json:"surgeMultiplier"`
	DiscountTiers   []DiscountTier    `json:"discountTiers"`
}

// DiscountTier defines volume discounts
type DiscountTier struct {
	MinHours uint64  `json:"minHours"`
	Discount float64 `json:"discount"` // Percentage
}

// Pricing contains provider-specific pricing
type Pricing struct {
	HourlyRate   uint64  `json:"hourlyRate"`
	MinDuration  time.Duration `json:"minDuration"`
	MaxDuration  time.Duration `json:"maxDuration"`
	Currency     string  `json:"currency"` // "LUX"
}

// QualityRequirement defines quality requirements
type QualityRequirement struct {
	MinReputation   float64 `json:"minReputation"`
	MaxFailureRate  float64 `json:"maxFailureRate"`
	MinUptime       float64 `json:"minUptime"`
	RequireSLA      bool    `json:"requireSla"`
}

// ProviderStatus represents provider status
type ProviderStatus struct {
	Online       bool      `json:"online"`
	LastSeen     time.Time `json:"lastSeen"`
	Uptime       float64   `json:"uptime"`
	FailureRate  float64   `json:"failureRate"`
	CurrentTasks int       `json:"currentTasks"`
	MaxTasks     int       `json:"maxTasks"`
}

// GPUSubnetManager manages GPU subnets
type GPUSubnetManager struct {
	subnets  map[ids.ID]*GPUSubnet
	aivm     *VM
	mu       sync.RWMutex
	log      logging.Logger
}

// NewGPUSubnetManager creates a new GPU subnet manager
func NewGPUSubnetManager(aivm *VM, log logging.Logger) *GPUSubnetManager {
	return &GPUSubnetManager{
		subnets: make(map[ids.ID]*GPUSubnet),
		aivm:    aivm,
		log:     log,
	}
}

// CreateSubnet creates a new GPU subnet
func (m *GPUSubnetManager) CreateSubnet(name string, config GPUSubnetConfig) (*GPUSubnet, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	subnetID := ids.GenerateTestID()
	
	subnet := &GPUSubnet{
		SubnetID:    subnetID,
		Name:        name,
		Config:      config,
		Providers:   []GPUProvider{},
		Tasks:       []GPUTask{},
		Performance: SubnetMetrics{},
		log:         m.log,
	}

	m.subnets[subnetID] = subnet
	
	m.log.Info("GPU subnet created",
		zap.String("subnetID", subnetID.String()),
		zap.String("name", name),
		zap.Int("minGPUs", config.MinGPUCount),
	)

	return subnet, nil
}

// RegisterProvider registers a GPU provider to a subnet
func (s *GPUSubnet) RegisterProvider(nodeID ids.NodeID, info ProviderInfo, gpus []GPUSpec) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate GPU specifications
	validGPUs := 0
	for _, gpu := range gpus {
		for _, requiredType := range s.Config.RequiredGPUTypes {
			if gpu.Model == requiredType && gpu.Available {
				validGPUs++
				break
			}
		}
	}

	if validGPUs < s.Config.MinGPUCount {
		return errors.New("insufficient valid GPUs")
	}

	provider := GPUProvider{
		NodeID:       nodeID,
		ProviderInfo: info,
		GPUs:         gpus,
		Status: ProviderStatus{
			Online:   true,
			LastSeen: time.Now(),
			Uptime:   100.0,
			MaxTasks: validGPUs * 2, // 2 tasks per GPU
		},
		Reputation: 100.0, // Start with perfect reputation
		JoinedAt:   time.Now(),
	}

	s.Providers = append(s.Providers, provider)
	s.Performance.ActiveProviders++

	s.log.Info("GPU provider registered",
		zap.String("nodeID", nodeID.String()),
		zap.Int("gpuCount", len(gpus)),
		zap.String("location", info.Location),
	)

	return nil
}

// SubmitTask submits a new GPU task
func (s *GPUSubnet) SubmitTask(ctx context.Context, taskType string, requirements TaskRequirements, requester ids.ShortID) (*GPUTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate task type
	validType := false
	for _, allowed := range s.Config.TaskTypes {
		if taskType == allowed {
			validType = true
			break
		}
	}
	if !validType {
		return nil, errors.New("unsupported task type")
	}

	// Find suitable provider
	provider, err := s.findSuitableProvider(requirements)
	if err != nil {
		return nil, err
	}

	task := &GPUTask{
		TaskID:       ids.GenerateTestID(),
		Type:         taskType,
		Requirements: requirements,
		Requester:    requester,
		Provider:     provider.NodeID,
		Status:       TaskPending,
		CreatedAt:    time.Now(),
	}

	s.Tasks = append(s.Tasks, *task)
	s.Performance.TotalTasks++

	// Update provider status
	for i, p := range s.Providers {
		if p.NodeID == provider.NodeID {
			s.Providers[i].Status.CurrentTasks++
			break
		}
	}

	s.log.Info("GPU task submitted",
		zap.String("taskID", task.TaskID.String()),
		zap.String("type", taskType),
		zap.String("provider", provider.NodeID.String()),
	)

	// Start task execution in background
	go s.executeTask(task)

	return task, nil
}

// findSuitableProvider finds a provider that meets requirements
func (s *GPUSubnet) findSuitableProvider(req TaskRequirements) (*GPUProvider, error) {
	var candidates []GPUProvider

	for _, provider := range s.Providers {
		if !provider.Status.Online {
			continue
		}

		// Check reputation
		if provider.Reputation < s.Config.QualityMetrics.MinReputation {
			continue
		}

		// Check GPU requirements
		hasValidGPU := false
		for _, gpu := range provider.GPUs {
			if gpu.Available && 
			   gpu.Memory >= req.MinMemory &&
			   gpu.Performance >= req.MinCompute {
				if req.GPUModel == "" || gpu.Model == req.GPUModel {
					hasValidGPU = true
					break
				}
			}
		}

		if hasValidGPU && provider.Status.CurrentTasks < provider.Status.MaxTasks {
			candidates = append(candidates, provider)
		}
	}

	if len(candidates) == 0 {
		return nil, errors.New("no suitable providers available")
	}

	// Select provider with best reputation and lowest load
	best := &candidates[0]
	for i := 1; i < len(candidates); i++ {
		loadCurrent := float64(best.Status.CurrentTasks) / float64(best.Status.MaxTasks)
		loadCandidate := float64(candidates[i].Status.CurrentTasks) / float64(candidates[i].Status.MaxTasks)
		
		if candidates[i].Reputation > best.Reputation || 
		   (candidates[i].Reputation == best.Reputation && loadCandidate < loadCurrent) {
			best = &candidates[i]
		}
	}

	return best, nil
}

// executeTask simulates task execution
func (s *GPUSubnet) executeTask(task *GPUTask) {
	// Simulate task execution
	time.Sleep(5 * time.Second)

	s.mu.Lock()
	defer s.mu.Unlock()

	// Find task and update
	for i, t := range s.Tasks {
		if t.TaskID == task.TaskID {
			s.Tasks[i].Status = TaskCompleted
			s.Tasks[i].StartedAt = task.CreatedAt.Add(1 * time.Second)
			s.Tasks[i].CompletedAt = time.Now()
			
			// Mock result
			s.Tasks[i].Result = GPUTaskResult{
				Success: true,
				Metrics: ComputeMetrics{
					ExecutionTime: 4 * time.Second,
					GPUTime:       3 * time.Second,
					MemoryUsed:    8 * 1024 * 1024 * 1024, // 8GB
					PowerUsed:     300, // 300W
					Throughput:    1000, // 1000 inferences/sec
				},
				ProofOfWork: []byte("mock_proof"),
			}

			// Update metrics
			s.Performance.CompletedTasks++
			s.Performance.TotalGPUHours += 3.0 / 3600.0 // 3 seconds
			
			// Update provider
			for j, p := range s.Providers {
				if p.NodeID == task.Provider {
					s.Providers[j].Status.CurrentTasks--
					s.Providers[j].TasksHandled++
					break
				}
			}
			
			break
		}
	}
}

// GetSubnetStats returns subnet statistics
func (s *GPUSubnet) GetSubnetStats() SubnetMetrics {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Calculate average latency
	var totalLatency time.Duration
	completedCount := 0
	
	for _, task := range s.Tasks {
		if task.Status == TaskCompleted {
			latency := task.CompletedAt.Sub(task.CreatedAt)
			totalLatency += latency
			completedCount++
		}
	}

	if completedCount > 0 {
		s.Performance.AverageLatency = totalLatency / time.Duration(completedCount)
	}

	// Calculate network hashrate (simplified)
	totalTFLOPS := float64(0)
	for _, provider := range s.Providers {
		if provider.Status.Online {
			for _, gpu := range provider.GPUs {
				if gpu.Available {
					totalTFLOPS += gpu.Performance
				}
			}
		}
	}
	s.Performance.NetworkHashrate = totalTFLOPS

	return s.Performance
}

// TestGPUSubnet creates a test GPU subnet configuration
func TestGPUSubnet() *GPUSubnetConfig {
	return &GPUSubnetConfig{
		MinGPUCount:      2,
		RequiredGPUTypes: []string{"RTX 4090", "A100", "H100"},
		TaskTypes:        []string{"training", "inference", "rendering", "mining"},
		PricingModel: PricingModel{
			BasePrice:       100 * 1e9, // 100 LUX per GPU hour
			PricePerTFLOPS:  10 * 1e9,  // 10 LUX per TFLOPS
			SurgeMultiplier: 1.5,
			DiscountTiers: []DiscountTier{
				{MinHours: 10, Discount: 5},
				{MinHours: 100, Discount: 10},
				{MinHours: 1000, Discount: 20},
			},
		},
		QualityMetrics: QualityRequirement{
			MinReputation:  80.0,
			MaxFailureRate: 0.05,
			MinUptime:      0.95,
			RequireSLA:     true,
		},
		NetworkTopology: "mesh",
	}
}