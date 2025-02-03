// Exports test metrics to DD so we can monitor for flaky tests over time
package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/DataDog/datadog-go/statsd"
	"gotest.tools/gotestsum/testjson"
)

var GLOBAL_STATSD *statsd.Client = nil

var NUM_TEST_RETRIES map[string]int

var UNUSED_GOLDENS []string = []string{
	"testCustomColumns-query-isAction=false", "testCustomColumns-tquery-bash",
	"testCustomColumns-tquery-zsh", "TestTuiBench-Query",
}

func main() {
	if os.Args[1] == "export" {
		exportMetrics()
	}
	if os.Args[1] == "check-goldens" {
		checkGoldensUsed()
	}
}

func checkGoldensUsed() {
	if os.Getenv("HISHTORY_FILTERED_TEST") != "" {
		return
	}
	// Read the goldens that were used
	usedGoldens := make([]string, 0)
	filenames, err := filepath.Glob("*/goldens-used.txt")
	if err != nil {
		log.Fatalf("failed to list golden files: %v", err)
	}
	fmt.Printf("Found used goldens in %#v\n", filenames)
	for _, filename := range filenames {
		usedGoldensFile, err := os.Open(filename)
		if err != nil {
			log.Fatalf("failed to open %s: %v", filename, err)
		}
		defer usedGoldensFile.Close()
		scanner := bufio.NewScanner(usedGoldensFile)
		for scanner.Scan() {
			usedGoldens = append(usedGoldens, strings.TrimSpace(scanner.Text()))
		}
		if err := scanner.Err(); err != nil {
			log.Fatalf("failed to read lines from /tmp/goldens-used.txt: %v", err)
		}
	}

	// List all the goldens that exist
	goldensDir := "client/testdata/"
	files, err := os.ReadDir(goldensDir)
	if err != nil {
		panic(fmt.Errorf("failed to list files in %s: %w", goldensDir, err))
	}

	// And check for mismatches
	var unusedGoldenErr error = nil
	for _, f := range files {
		goldenName := path.Base(f.Name())
		if !slices.Contains(usedGoldens, goldenName) {
			if slices.Contains(UNUSED_GOLDENS, goldenName) {
				// It is allowlisted to not be used
				continue
			}
			unusedGoldenErr = fmt.Errorf("golden file %v was never used", goldenName)
			fmt.Println(unusedGoldenErr)
		}
	}
	if unusedGoldenErr != nil {
		log.Fatalf("%v", unusedGoldenErr)
	}

	// And print out anything that is in UNUSED_GOLDENS that was actually used, so we
	// can manually trim UNUSED_GOLDENS
	for _, g := range UNUSED_GOLDENS {
		if slices.Contains(usedGoldens, g) {
			fmt.Printf("Golden %s is in UNUSED_GOLDENS, but was actually used\n", g)
		}
	}
	fmt.Println("Validated that all goldens in testdata/ were referenced!")
}

func exportMetrics() {
	// Configure Datadog
	if _, has_dd_api_key := os.LookupEnv("DD_API_KEY"); has_dd_api_key {
		ddStats, err := statsd.New("localhost:8125")
		if err != nil {
			err := fmt.Errorf("failed to start DataDog statsd: %w", err)
			if runtime.GOOS == "darwin" {
				fmt.Printf("failed to init datadog: %v", err)
				os.Exit(0)
			} else {
				log.Fatalf("failed to init datadog: %v", err)
			}
		}
		defer ddStats.Close()
		GLOBAL_STATSD = ddStats
	} else {
		fmt.Printf("Skipping exporting test stats to datadog\n")
	}

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
		err := GLOBAL_STATSD.Flush()
		if err != nil {
			log.Fatalf("failed to flush metrics: %v", err)
		}
		fmt.Printf("Uploaded data about %d tests to datadog\n", len(NUM_TEST_RETRIES))
	}
}

type eventHandler struct{}

func (eventHandler) Event(event testjson.TestEvent, execution *testjson.Execution) error {
	testIdentifier := event.Test
	if event.Action == testjson.ActionFail {
		fmt.Println("Recorded failure for " + testIdentifier)
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
