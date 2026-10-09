// Command fakerestic imitates restic for tests. Behaviour is chosen by FAKE_MODE.
package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

func main() {
	if f := os.Getenv("FAKE_ARGS_FILE"); f != "" {
		line := strings.Join(os.Args[1:], " ") + "\nREPO=" + os.Getenv("RESTIC_REPOSITORY") + "\nPW=" + os.Getenv("RESTIC_PASSWORD") + "\nAMBIENT_PWCMD=" + os.Getenv("RESTIC_PASSWORD_COMMAND") + "\n"
		_ = os.WriteFile(f, []byte(line), 0o600)
	}
	switch os.Getenv("FAKE_MODE") {
	case "version":
		fmt.Println("restic 0.19.1 compiled with go1.26 on windows/amd64")
	case "old":
		fmt.Println("restic 0.14.0 compiled with go1.19 on windows/amd64")
	case "exit12":
		fmt.Fprintln(os.Stderr, "Fatal: wrong password or no key found")
		os.Exit(12)
	case "exit10":
		fmt.Fprintln(os.Stderr, "Fatal: repository does not exist")
		os.Exit(10)
	case "exit11":
		fmt.Fprintln(os.Stderr, "Fatal: unable to create lock in backend")
		os.Exit(11)
	case "legacy-wrongpw":
		fmt.Fprintln(os.Stderr, "Fatal: wrong password or no key found")
		os.Exit(1)
	case "leak":
		fmt.Fprintln(os.Stderr, "Fatal: cannot open rest:https://bob:s3cretpw@example.org/ with RESTIC_PASSWORD="+os.Getenv("RESTIC_PASSWORD"))
		os.Exit(1)
	case "sleep":
		time.Sleep(time.Hour)
	case "snapshots":
		fmt.Println(`[{"time":"2026-01-02T03:04:05Z","paths":["C:/data"],"hostname":"h","id":"bbbb0000bbbb0000","short_id":"bbbb0000"},{"time":"2025-01-02T03:04:05Z","paths":["C:/old"],"hostname":"h","id":"aaaa0000aaaa0000","short_id":"aaaa0000"}]`)
	case "bigjson":
		fmt.Print(strings.Repeat("x", 5000))
	case "longline":
		fmt.Println(strings.Repeat("y", 3<<20))
		fmt.Println(`{"message_type":"summary","total_files":3,"files_restored":3,"total_bytes":9,"bytes_restored":9}`)
	case "restore-partial":
		fmt.Println(`{"message_type":"status","percent_done":0.5,"total_files":4,"files_restored":2,"total_bytes":100,"bytes_restored":50}`)
		fmt.Fprintln(os.Stderr, `{"message_type":"error","error":{"message":"failed to restore timestamp of C:/x: Access is denied."},"during":"restore","item":"/C/x"}`)
		fmt.Fprintln(os.Stderr, `{"message_type":"error","error":{"message":"unable to write file"},"during":"restore","item":"/C/y"}`)
		fmt.Println(`{"message_type":"summary","total_files":4,"files_restored":2,"total_bytes":100,"bytes_restored":50}`)
		os.Exit(1)
	}
}
