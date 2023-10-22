// Exports test metrics to DD so we can monitor for flaky tests over time
package main

import (
	"fmt"
	"log"
	"os"
	"runtime"

	"github.com/DataDog/datadog-go/statsd"
	"gotest.tools/gotestsum/testjson"
)

var GLOBAL_STATSD *statsd.Client

var NUM_TEST_RETRIES map[string]int

func main() {
	// Configure Datadog
	if _, has_dd_api_key := os.LookupEnv("DD_API_KEY"); !(has_dd_api_key) {
		fmt.Printf("Skipping exporting test stats to datadog\n")
	}
	ddStats, err := statsd.New("localhost:8125")
	if err != nil {
		err := fmt.Errorf("failed to start DataDog statsd: %w", err)
		if runtime.GOOS == "darwin" {
			fmt.Printf("failed init datadog: %v", err)
			os.Exit(0)
		} else {
			log.Fatalf("failed to init datadog: %v", err)
		}
	}
	GLOBAL_STATSD = ddStats

	// Parse the test output
	NUM_TEST_RETRIES = make(map[string]int)
	inputFile, err := os.Open("/tmp/testrun.json")
	if err != nil {
		log.Fatalf("failed to open test input file: %v", err)
	}
	_, err = testjson.ScanTestOutput(testjson.ScanConfig{
		Stdout:  inputFile,
		Handler: eventHandler{},
	})
	if err != nil {
		log.Fatalf("failed to scan testjson: %v", err)
	}
	for testId, count := range NUM_TEST_RETRIES {
		GLOBAL_STATSD.Distribution("test_retry_count", float64(count), []string{"test:" + testId, "os:" + runtime.GOOS}, 1.0)
	}
	if GLOBAL_STATSD == nil {
		fmt.Printf("Skipped uploading data about %d tests to datadog because GLOBAL_STATSD==nil\n", len(NUM_TEST_RETRIES))
	} else {
		fmt.Printf("Uploaded data about %d tests to datadog\n", len(NUM_TEST_RETRIES))
	}
}

type eventHandler struct{}

func (eventHandler) Event(event testjson.TestEvent, execution *testjson.Execution) error {
	testIdentifier := fmt.Sprintf("%s#%s", event.Package, event.Test)
	if event.Action == testjson.ActionFail {
		GLOBAL_STATSD.Incr("test_status", []string{"result:failed", "test:" + testIdentifier, "os:" + runtime.GOOS}, 1.0)
		NUM_TEST_RETRIES[testIdentifier] += 1
	}
	if event.Action == testjson.ActionPass {
		GLOBAL_STATSD.Distribution("test_runtime", event.Elapsed, []string{"test:" + testIdentifier, "os:" + runtime.GOOS}, 1.0)
		GLOBAL_STATSD.Incr("test_status", []string{"result:passed", "test:" + testIdentifier, "os:" + runtime.GOOS}, 1.0)
		NUM_TEST_RETRIES[testIdentifier] += 1
	}
	return nil
}

func (eventHandler) Err(text string) error {
	return fmt.Errorf("unexpected error when parsing test output: %v", text)
}
