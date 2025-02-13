package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cucumber/godog"
	_ "github.com/mattn/go-sqlite3"
)

// VectorClockAgent collects timings for steps and persists them to SQLite.
type VectorClockAgent struct {
	startTimes sync.Map
	durations  sync.Map
	counter    uint64
	db         *sql.DB
}

func NewVectorClockAgent(dbPath string) *VectorClockAgent {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		panic(fmt.Sprintf("failed to open SQLite database: %v", err))
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS step_timings (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			step_id TEXT UNIQUE,
			scenario_name TEXT,
			step_text TEXT,
			duration_ms INTEGER,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		panic(fmt.Sprintf("failed to create table: %v", err))
	}

	return &VectorClockAgent{
		db: db,
	}
}

func (v *VectorClockAgent) generateStepID(scenarioName, stepText string) string {
	count := atomic.AddUint64(&v.counter, 1)
	return fmt.Sprintf("%s-%s-%d", scenarioName, stepText, count)
}

func (v *VectorClockAgent) Start(scenarioName, stepText string) string {
	stepID := v.generateStepID(scenarioName, stepText)
	v.startTimes.Store(stepID, time.Now())
	return stepID
}

func (v *VectorClockAgent) End(stepID, scenarioName, stepText string) {
	val, ok := v.startTimes.Load(stepID)
	if !ok {
		fmt.Printf("No start time recorded for step '%s'\n", stepID)
		return
	}
	startTime, _ := val.(time.Time)
	duration := time.Since(startTime)
	v.durations.Store(stepID, duration)

	_, err := v.db.Exec(`
		INSERT OR IGNORE INTO step_timings (step_id, scenario_name, step_text, duration_ms)
		VALUES (?, ?, ?, ?)
	`, stepID, scenarioName, stepText, duration.Milliseconds())

	if err != nil {
		fmt.Printf("Failed to save step '%s' to DB: %v\n", stepID, err)
	}
}

func (v *VectorClockAgent) Report() {
	fmt.Println("=== Step Duration Report (SQLite) ===")
	rows, err := v.db.Query(`SELECT step_id, scenario_name, step_text, duration_ms, created_at FROM step_timings`)
	if err != nil {
		fmt.Printf("Failed to fetch report: %v\n", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var stepID, scenarioName, stepText, createdAt string
		var durationMs int64
		if err := rows.Scan(&stepID, &scenarioName, &stepText, &durationMs, &createdAt); err != nil {
			fmt.Printf("Failed to scan row: %v\n", err)
			continue
		}
		fmt.Printf("StepID: %s, Scenario: %s, Step: %s, Duration: %d ms, Timestamp: %s\n", stepID, scenarioName, stepText, durationMs, createdAt)
	}
}

func (v *VectorClockAgent) Close() error {
	return v.db.Close()
}

var agent *VectorClockAgent

func InitializeScenario(ctx *godog.ScenarioContext) {
	var scenarioName string

	ctx.Before(func(ctx context.Context, s *godog.Scenario) (context.Context, error) {
		scenarioName = s.Name
		return ctx, nil
	})

	stepIDs := make(map[*godog.Step]string)
	stepCtx := ctx.StepContext()

	stepCtx.Before(func(ctx context.Context, step *godog.Step) (context.Context, error) {
		stepID := agent.Start(scenarioName, step.Text)
		stepIDs[step] = stepID
		return ctx, nil
	})

	stepCtx.After(func(ctx context.Context, step *godog.Step, status godog.StepResultStatus, err error) (context.Context, error) {
		if stepID, ok := stepIDs[step]; ok {
			agent.End(stepID, scenarioName, step.Text)
			delete(stepIDs, step)
		}
		return ctx, nil
	})

	ctx.Step(`^I perform an action$`, iPerformAction)
}

func iPerformAction() error {
	time.Sleep(150 * time.Millisecond)
	return nil
}

func main() {
	agent = NewVectorClockAgent("step_timings.db")

	opts := godog.Options{
		Format: "pretty",
		Paths:  []string{"features"},
	}

	suite := godog.TestSuite{
		Name:                "godogsuite",
		ScenarioInitializer: InitializeScenario,
		Options:             &opts,
	}

	status := suite.Run()

	agent.Report()
	agent.Close()

	if status != 0 {
		os.Exit(status)
	}
}
