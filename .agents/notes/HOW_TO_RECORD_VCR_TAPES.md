# How to record glm-5.1 VCR tapes

Use this note when `internal/agent/testdata/TestCoderAgent/glm-5.1/*.yaml`
needs to be regenerated.

## Important context

- The tests record live requests through Hyper, so `CRUSH_HYPER_API_KEY` must be
  set.
- Hyper / GLM-5.1 can be finicky. A test that times out once may pass quickly on
  retry.
- Slow recordings we observed were not caused by filesystem sandbox problems:
  `read_a_file` successfully called `glob` and received the `/tmp/crush-test/...`
  tool result, then stalled waiting for the next model response.
- Test workspaces are created under `/tmp/crush-test/<test-name>/`; the SQLite DB
  uses `t.TempDir()`.
- Replay should be fast. After recording, verify with:

  ```bash
  go test ./internal/agent -run '^TestCoderAgent/glm-5\.1$' -count=1 -timeout=5m
  ```

## Recommended command

Run the helper script from the repository root. With no arguments, it derives
subtest names from the existing `.yaml` files in the cassette directory:

```bash
node .agents/notes/record_tapes.mjs
```

To record only specific cassettes, pass their subtest names:

```bash
node .agents/notes/record_tapes.mjs read_a_file update_a_file download_tool
```

The script logs timestamped progress before and after each test, so it is safe
to run in the background and inspect with the job output tool.

## Methodology implemented by the script

1. Try every requested tape with a 1-minute per-test timeout.
2. Keep successful recordings and defer tests that time out or fail.
3. Retry the deferred list with doubled per-test timeouts:
   - 60 seconds
   - 120 seconds
   - 240 seconds
   - 300 seconds maximum
4. If any test fails at the 5-minute timeout, abort and print the failing tests.
5. If all tapes record, verify replay for the full `glm-5.1` subtest.

The script deletes each cassette before running its corresponding subtest, which
forces VCR `ModeRecordOnce` to record fresh live interactions.

## Manual one-off command

```bash
rm -f internal/agent/testdata/TestCoderAgent/glm-5.1/<cassette_name>.yaml
go test ./internal/agent -run '^TestCoderAgent/glm-5\.1/<subtest_name>$' -count=1 -timeout=10m -v
```

Example:

```bash
rm -f internal/agent/testdata/TestCoderAgent/glm-5.1/read_a_file.yaml
go test ./internal/agent -run '^TestCoderAgent/glm-5\.1/read_a_file$' -count=1 -timeout=10m -v
```

## Current known subtests

- `simple_test`
- `read_a_file`
- `update_a_file`
- `bash_tool`
- `download_tool`
- `fetch_tool`
- `glob_tool`
- `grep_tool`
- `ls_tool`
- `multiedit_tool`
- `sourcegraph_tool`
- `write_tool`
- `parallel_tool_calls`
