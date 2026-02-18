#!/usr/bin/env bash
set -euo pipefail

PROVIDER="${QU_AGENTIC_SMOKE_PROVIDER:-azopenai}"
MODEL="${QU_AGENTIC_SMOKE_MODEL:-gemini-2.5-flash}"
PROMPT="${QU_AGENTIC_SMOKE_PROMPT:-summarize cluster health in one short paragraph}"
BIN="${QU_AGENTIC_SMOKE_BIN:-./kubectl-quackops}"

# Advanced MCP loop tuning stays env-only on purpose.
MCP_MAX_TOOL_CALLS_TOTAL="${QU_AGENTIC_SMOKE_MCP_MAX_TOOL_CALLS_TOTAL:-10}"
MCP_TOOL_REPEAT_LIMIT="${QU_AGENTIC_SMOKE_MCP_TOOL_REPEAT_LIMIT:-2}"
MCP_LOOP_CYCLE_THRESHOLD="${QU_AGENTIC_SMOKE_MCP_LOOP_CYCLE_THRESHOLD:-2}"
MCP_NO_PROGRESS_THRESHOLD="${QU_AGENTIC_SMOKE_MCP_NO_PROGRESS_THRESHOLD:-1}"

if [[ ! -x "${BIN}" ]]; then
  echo "binary not found or not executable: ${BIN}" >&2
  echo "build it first with: make build" >&2
  exit 1
fi

tmp_log="$(mktemp)"
trap 'rm -f "${tmp_log}"' EXIT

has_match() {
  local pattern="$1"
  if command -v rg >/dev/null 2>&1; then
    rg -q "${pattern}" "${tmp_log}"
  else
    grep -E -q "${pattern}" "${tmp_log}"
  fi
}

set +e
DEBUG=1 \
QU_MCP_MAX_TOOL_CALLS_TOTAL="${MCP_MAX_TOOL_CALLS_TOTAL}" \
QU_MCP_TOOL_REPEAT_LIMIT="${MCP_TOOL_REPEAT_LIMIT}" \
QU_MCP_LOOP_CYCLE_THRESHOLD="${MCP_LOOP_CYCLE_THRESHOLD}" \
QU_MCP_NO_PROGRESS_THRESHOLD="${MCP_NO_PROGRESS_THRESHOLD}" \
"${BIN}" \
  -p "${PROVIDER}" \
  -m "${MODEL}" \
  --disable-baseline \
  --mcp-client=true \
  --mcp-strict=true \
  --mcp-max-tool-calls=5 \
  -- "${PROMPT}" >"${tmp_log}" 2>&1
status=$?
set -e

if [[ ${status} -ne 0 ]]; then
  echo "agentic loop smoke run failed (exit ${status})" >&2
  echo "provider=${PROVIDER} model=${MODEL}" >&2
  tail -n 60 "${tmp_log}" >&2
  exit ${status}
fi

if ! has_match "Processing MCP tool call"; then
  echo "smoke check failed: no MCP tool-call iteration marker found" >&2
  tail -n 80 "${tmp_log}" >&2
  exit 1
fi

if ! has_match "Executing MCP tool|MCP tool cache hit"; then
  echo "smoke check failed: no MCP tool execution/cache marker found" >&2
  tail -n 80 "${tmp_log}" >&2
  exit 1
fi

if ! has_match "MCP tool loop|MCP tool repeat limit|MCP total tool-call budget|MCP tool-result budget|MCP tool cache hit"; then
  echo "warning: no loop guard/cache marker observed; run may still be healthy" >&2
fi

echo "agentic loop smoke test passed"
echo "provider=${PROVIDER} model=${MODEL}"
