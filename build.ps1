wails build @args
if ($LASTEXITCODE -eq 0) {
    Copy-Item -Path "scripts" -Destination "build\bin\scripts" -Recurse -Force
    Write-Host "scripts/ copied to build\bin\scripts\" -ForegroundColor Green
}
