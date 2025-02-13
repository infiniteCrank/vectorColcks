package main

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cucumber/godog"
)

// VectorClockAgent collects timings for steps.
type VectorClockAgent struct {
	startTimes sync.Map // key: string (unique step id), value: time.Time
	durations  sync.Map // key: string (unique step id), value: time.Duration
	counter    uint64   // atomic counter for unique ID generation
}

// NewVectorClockAgent creates a new timing agent.
func NewVectorClockAgent() *VectorClockAgent {
	return &VectorClockAgent{}
}

// generateStepID creates a unique identifier using scenario name, step text, and an atomic counter.
func (v *VectorClockAgent) generateStepID(scenarioName, stepText string) string {
	count := atomic.AddUint64(&v.counter, 1)
	return fmt.Sprintf("%s-%s-%d", scenarioName, stepText, count)
}

// Start records the start time for a step and returns its unique ID.
func (v *VectorClockAgent) Start(scenarioName, stepText string) string {
	stepID := v.generateStepID(scenarioName, stepText)
	v.startTimes.Store(stepID, time.Now())
	return stepID
}

// End records the end time for a step, computes the duration, and logs it.
func (v *VectorClockAgent) End(stepID string) {
	val, ok := v.startTimes.Load(stepID)
	if !ok {
		fmt.Printf("No start time recorded for step '%s'\n", stepID)
		return
	}
	startTime, ok := val.(time.Time)
	if !ok {
		fmt.Printf("Invalid start time type for step '%s'\n", stepID)
		return
	}
	duration := time.Since(startTime)
	v.durations.Store(stepID, duration)
	fmt.Printf("Step '%s' took %v\n", stepID, duration)
}

// Report prints a summary of all step durations.
func (v *VectorClockAgent) Report() {
	fmt.Println("=== Step Duration Report ===")
	v.durations.Range(func(key, value interface{}) bool {
		stepID, ok := key.(string)
		if !ok {
			return true
		}
		duration, ok := value.(time.Duration)
		if !ok {
			return true
		}
		fmt.Printf("Step: %s, Duration: %v\n", stepID, duration)
		return true
	})
}

// Global agent instance shared across scenarios.
var agent = NewVectorClockAgent()

// InitializeScenario registers steps and hooks using the new godog API.
func InitializeScenario(ctx *godog.ScenarioContext) {
	var scenarioName string

	// BeforeScenario hook with the new signature.
	ctx.Before(func(ctx context.Context, s *godog.Scenario) (context.Context, error) {
		scenarioName = s.Name
		return ctx, nil
	})

	// Local map to correlate each step to its generated unique ID.
	stepIDs := make(map[*godog.Step]string)

	// Use StepContext() to register step-level hooks.
	stepCtx := ctx.StepContext()

	stepCtx.Before(func(ctx context.Context, step *godog.Step) (context.Context, error) {
		stepID := agent.Start(scenarioName, step.Text)
		stepIDs[step] = stepID
		return ctx, nil
	})

	stepCtx.After(func(ctx context.Context, step *godog.Step, status godog.StepResultStatus, err error) (context.Context, error) {
		if stepID, ok := stepIDs[step]; ok {
			agent.End(stepID)
			delete(stepIDs, step)
		} else {
			fmt.Printf("Step ID not found for step: %s\n", step.Text)
		}
		return ctx, nil
	})

	// Register your step definitions.
	ctx.Step(`^I perform an action$`, iPerformAction)
}

// Example step function.
func iPerformAction() error {
	// Simulate some work.
	time.Sleep(150 * time.Millisecond)
	return nil
}

func main() {
	opts := godog.Options{
		Format: "pretty",             // or your preferred format
		Paths:  []string{"features"}, // adjust to your feature file paths
	}

	suite := godog.TestSuite{
		Name:                "godogsuite",
		ScenarioInitializer: InitializeScenario,
		Options:             &opts,
	}

	status := suite.Run()

	// Optionally, print the timing report.
	agent.Report()

	if status != 0 {
		fmt.Printf("Tests failed with status: %d\n", status)
		os.Exit(status)
	}
}
