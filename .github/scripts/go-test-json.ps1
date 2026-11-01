# Run a selected go test and reject an empty match.
# Skip results are printed apart from passes and are not successes by themselves.
# Arguments are go test flags and packages, without -json. Example:
#   .\.github\scripts\go-test-json.ps1 -race -count=1 -cover ./internal/adapter -run '^TestFoo$'

$ErrorActionPreference = "Stop"
$PSNativeCommandUseErrorActionPreference = $false
Set-StrictMode -Version Latest

if ($args.Count -lt 1) {
    Write-Error "go-test-json: missing go test arguments"
    exit 2
}
if (-not (Test-Path -LiteralPath ".\citestjson.exe")) {
    Write-Error "go-test-json: .\citestjson.exe is missing; build ./tools/citestjson first"
    exit 2
}

& go test -json @args | & .\citestjson.exe
exit $LASTEXITCODE
