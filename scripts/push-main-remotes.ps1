# Push main to GitLab (CI canonical) then GitHub mirror — one command for local dev.
# Requires Git Credential Manager / PAT auth already configured for each remote.
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
Set-Location (Resolve-Path (Join-Path $PSScriptRoot '..'))

function Push-IfRemote([string]$RemoteName) {
    git remote get-url $RemoteName 2>$null | Out-Null
    if ($LASTEXITCODE -ne 0) {
        Write-Host "skip: remote '$RemoteName' not configured"
        return
    }
    Write-Host "git push $RemoteName main"
    git push $RemoteName main
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
}

Push-IfRemote 'gitlab'
Push-IfRemote 'origin'
