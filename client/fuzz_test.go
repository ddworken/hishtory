package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/ddworken/hishtory/shared/testutils"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
)

type operation struct {
	device      device
	cmd         string
	redactQuery string
}

var tmp int = 0
var runCounter *int = &tmp

func fuzzTest(t *testing.T, tester shellTester, input string) {
	testutils.TestLog(t, fmt.Sprintf("Starting fuzz test for input=%#v", input))
	*runCounter += 1
	// Parse the input
	if len(input) > 1_000 {
		return
	}
	input = strings.TrimSpace(input)
	ops := make([]operation, 0)
	for _, line := range strings.Split(input, "\n") {
		split1 := strings.SplitN(line, "|", 2)
		if len(split1) != 2 {
			panic("malformed: split1")
		}
		split2 := strings.SplitN(split1[0], ";", 2)
		if len(split2) != 2 {
			panic("malformed: split2")
		}
		unparsedOperation := split1[1]
		cmd := ""
		redactQuery := ""
		if strings.HasPrefix(unparsedOperation, "!") {
			redactQuery = unparsedOperation[1:]
		} else {
			cmd = "echo " + unparsedOperation
		}
		re := regexp.MustCompile(`[a-zA-Z]+`)
		if !re.MatchString(cmd) && cmd != "" {
			panic("malformed: re")
		}
		key := split2[0]
		if strings.Contains(key, "-") {
			panic("malformed: key-")
		}
		op := operation{device: device{key: key + "-" + strconv.Itoa(*runCounter), deviceId: split2[1]}, cmd: cmd, redactQuery: redactQuery}
		ops = append(ops, op)
	}

	// Set up and create the devices
	defer testutils.BackupAndRestore(t)()
	var deviceMap map[device]deviceOp = make(map[device]deviceOp)
	var devices deviceSet = deviceSet{}
	devices.deviceMap = &deviceMap
	devices.currentDevice = nil
	for _, op := range ops {
		_, ok := (*devices.deviceMap)[op.device]
		if ok {
			continue
		}
		createDevice(t, tester, &devices, op.device.key, op.device.deviceId)
	}

	// Persist our basic in-memory copy of expected shell commands
	keyToCommands := make(map[string]string)

	// Run the commands
	for _, op := range ops {
		testutils.TestLog(t, fmt.Sprintf("Running op=%#v", op))
		// Run the command
		switchToDevice(&devices, op.device)
		if op.cmd != "" {
			_, err := tester.RunInteractiveShellRelaxed(t, op.cmd)
			require.NoError(t, err)
		}
		if op.redactQuery != "" {
			_, err := tester.RunInteractiveShellRelaxed(t, `HISHTORY_REDACT_FORCE=1 hishtory redact `+op.redactQuery)
			require.NoError(t, err)
		}

		// Calculate the expected output of hishtory export
		val, ok := keyToCommands[op.device.key]
		if !ok {
			val = ""
		}
		if op.cmd != "" {
			val += op.cmd
			val += "\n"
		}
		if op.redactQuery != "" {
			lines := strings.Split(val, "\n")
			filteredLines := make([]string, 0)
			for _, line := range lines {
				if strings.Contains(line, op.redactQuery) {
					continue
				}
				filteredLines = append(filteredLines, line)
			}
			val = strings.Join(filteredLines, "\n")
			val += `HISHTORY_REDACT_FORCE=1 hishtory redact ` + op.redactQuery + "\n"
		}
		keyToCommands[op.device.key] = val

		// Run hishtory export and check the output
		out, err := tester.RunInteractiveShellRelaxed(t, `hishtory export -export -pipefail`)
		require.NoError(t, err)
		expectedOutput := keyToCommands[op.device.key]
		if diff := cmp.Diff(expectedOutput, out); diff != "" {
			t.Fatalf("hishtory export mismatch for input=%#v key=%s (-expected +got):\n%s\nout=%#v", input, op.device.key, diff, out)
		}
		testutils.TestLog(t, fmt.Sprintf("Finished running op=%#v", op))
	}

	// Check that hishtory export has the expected results
	for _, op := range ops {
		switchToDevice(&devices, op.device)
		out, err := tester.RunInteractiveShellRelaxed(t, `hishtory export -export -pipefail`)
		require.NoError(t, err)
		expectedOutput := keyToCommands[op.device.key]
		if diff := cmp.Diff(expectedOutput, out); diff != "" {
			t.Fatalf("hishtory export mismatch for key=%s (-expected +got):\n%s\nout=%#v", op.device.key, diff, out)
		}
	}

	testutils.TestLog(t, fmt.Sprintf("Finished fuzz test for input=%#v", input))
}

func FuzzTestMultipleUsers(f *testing.F) {
	if skipSlowTests() {
		f.Skip("skipping slow tests")
	}
	s := os.Getenv("SPLIT_TESTS")
	if s != "" && s != "BASIC" {
		f.Skip()
	}
	defer testutils.RunTestServer()()
	// Format:
	//   $Op = $Key;$Device|$Command\n
	//         $Key;$Device|$Command\n$Op
	//   $Command = !$ThingToRedact
	//              $CommandToRun
	//
	// Running repeated commands
	f.Add("a;b|2\n")
	f.Add("a;b|aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n")
	f.Add("a;b|aaaBBcccDD\n")
	f.Add("a;a|hello\na;a|world")
	f.Add("a;a|hello\na;a|world\na;b|3")
	f.Add("a;a|1\na;a|2\na;b|3\nb;a|4\na;b|5")
	f.Add("a;a|1\na;a|2\na;b|1\n")
	f.Add("a;a|1\na;a|2\na;b|1\nz;z|1\na;a|1\n")
	f.Add("a;a|hello\na;a|wobld")
	f.Add("a;a|hello\na;a|hello")
	f.Add("a;a|1\nb;a|2\nc;a|2\nd;a|2\na;b|2\na;b|3\na;b|4\na;b|8\na;d|2\nb;a|1\n")
	f.Add("a;a|1\na;b|1\na;c|1\na;d|1\na;e|1\na;f|1\na;g|1\na;b|1\na;b|1\na;b|1\na;b|1\n")
	f.Add("a;a|1\nb;b|1\na;c|1\na;d|1\na;e|1\na;f|1\na;g|1\na;b|1\na;b|1\na;b|1\na;b|1\n")
	f.Add("a;a|1\na;a|1\na;c|1\na;d|1\na;e|1\na;f|1\na;g|1\na;b|1\na;b|1\na;b|1\na;b|1\n")
	f.Add("a;a|1\na;a|2\na;c|1\na;d|3\na;e|4\na;f|5\na;g|6\na;b|7\na;b|1\na;b|8\na;b|1\n")
	// Running repeated commands with redaction
	f.Add("a;b|!hello\n")
	f.Add("a;b|hello\na;b|world\na;b|!hello\n")
	f.Add("a;a|hello\na;a|world\na;b|!hello\na;b|hello\na;a|hell\na;a|hello\na;c|!hello\na;d|!hell\n")
	f.Add("a;b|hello\na;b|world\na;a|hello2\na;b|!hello\na;b|hello3\na;b|hello4\n")
	f.Add("a;b|hello\na;b|world\na;a|hello2\na;b|!h\na;b|!h\na;b|hello3\na;b|hello4\n")
	f.Add("a;a|1\na;a|2\na;c|1\na;d|3\na;e|4\na;f|5\na;g|6\na;b|7\na;b|1\na;b|8\na;b|1\na;a|!1\n")
	f.Fuzz(func(t *testing.T, input string) {
		fuzzTest(t, bashTester{}, input)
		fuzzTest(t, zshTester{}, input)
	})
}
