# Zebracat CLI installer for Windows (Windows PowerShell 5.1 and PowerShell 7+).
#   irm https://raw.githubusercontent.com/zebracatai/zebracat-cli/main/install.ps1 | iex
# (npm works too: npm install -g @zebracat/cli)
#
# Installs zebracat.exe into %LOCALAPPDATA%\Programs\zebracat and adds that folder
# to your user PATH. The binary comes from the project's GitHub Releases and is
# checked against the release's checksums.txt: no checksums file, no line for the
# archive, or a wrong sum, and nothing is installed.
#
# Env overrides:
#   ZEBRACAT_BIN_DIR          install dir (default: %LOCALAPPDATA%\Programs\zebracat)
#   ZEBRACAT_INSTALL_VERSION  pin a release tag (e.g. v0.7.0; default: latest)
#   ZEBRACAT_NO_MODIFY_PATH   set to 1 to leave your PATH as it is
#   ZEBRACAT_RELEASES_URL     releases host (default: https://github.com; for tests)

& {
  $ErrorActionPreference = 'Stop'
  $ProgressPreference = 'SilentlyContinue' # Invoke-WebRequest is many times slower with the progress bar

  $Repo = 'zebracatai/zebracat-cli'
  $Gh = if ($env:ZEBRACAT_RELEASES_URL) { $env:ZEBRACAT_RELEASES_URL.TrimEnd('/') } else { 'https://github.com' }
  $OnWindows = ($PSVersionTable.PSEdition -eq 'Desktop') -or $IsWindows

  function Say($msg) { Write-Host $msg -ForegroundColor Magenta }

  try {
    # Windows PowerShell 5.1 may default to TLS 1.0; GitHub needs 1.2.
    if ($PSVersionTable.PSEdition -eq 'Desktop') {
      [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
    }

    Say 'Installing the Zebracat CLI...'

    $osArch = $null
    try { $osArch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString() } catch { }
    if (-not $osArch) { $osArch = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE } }
    $arch = switch -Regex ($osArch) {
      '^(X64|AMD64)$' { 'amd64' }
      '^(Arm64|ARM64)$' { 'arm64' }
      default { throw "unsupported architecture: $osArch (64-bit Windows only)" }
    }

    $tag = $env:ZEBRACAT_INSTALL_VERSION
    if (-not $tag) {
      # GitHub redirects /releases/latest to /releases/tag/<tag>: reading that
      # redirect needs no API call (no 60/h anonymous rate limit).
      $req = [System.Net.HttpWebRequest]::Create("$Gh/$Repo/releases/latest")
      $req.AllowAutoRedirect = $false
      $req.Method = 'HEAD'
      $req.UserAgent = 'zebracat-install-ps1'
      try {
        $resp = $req.GetResponse()
      } catch [System.Net.WebException] {
        if (-not $_.Exception.Response) { throw "could not reach GitHub to find the latest release: $($_.Exception.Message)" }
        $resp = $_.Exception.Response
      }
      $location = $resp.Headers['Location']
      $resp.Close()
      if ($location -match '/releases/tag/([^/]+)/?$') { $tag = $Matches[1] }
    }
    if (-not $tag) { throw 'could not determine the latest release (none published yet?)' }

    $asset = "zebracat_windows_$arch.zip"
    $base = "$Gh/$Repo/releases/download/$tag"
    $tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("zebracat-" + [System.Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $tmp | Out-Null
    try {
      Say "Downloading $tag (windows/$arch)..."
      $zip = Join-Path $tmp $asset
      try { Invoke-WebRequest -Uri "$base/$asset" -OutFile $zip -UseBasicParsing }
      catch { throw "download failed: $base/$asset ($($_.Exception.Message))" }

      $sumsFile = Join-Path $tmp 'checksums.txt'
      try { Invoke-WebRequest -Uri "$base/checksums.txt" -OutFile $sumsFile -UseBasicParsing }
      catch { throw "could not download checksums.txt, so nothing was installed: $base/checksums.txt" }

      $want = $null
      foreach ($line in Get-Content -LiteralPath $sumsFile) {
        $parts = $line.Trim() -split '\s+'
        if ($parts.Count -eq 2 -and $parts[1] -eq $asset) { $want = $parts[0].ToLowerInvariant() }
      }
      if (-not $want) { throw "checksums.txt has no line for $asset; not installing" }
      $got = (Get-FileHash -LiteralPath $zip -Algorithm SHA256).Hash.ToLowerInvariant()
      if ($got -ne $want) { throw "checksum mismatch for $asset; the download was not installed" }
      Say 'Checksum verified'

      $out = Join-Path $tmp 'out'
      Expand-Archive -LiteralPath $zip -DestinationPath $out -Force
      $exe = Join-Path $out 'zebracat.exe'
      if (-not (Test-Path -LiteralPath $exe)) { throw 'the archive did not contain zebracat.exe' }

      $binDir = if ($env:ZEBRACAT_BIN_DIR) { $env:ZEBRACAT_BIN_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\zebracat' }
      New-Item -ItemType Directory -Force -Path $binDir | Out-Null
      $dest = Join-Path $binDir 'zebracat.exe'
      try { Copy-Item -LiteralPath $exe -Destination $dest -Force }
      catch { throw "could not write $dest (is zebracat running? close it and try again): $($_.Exception.Message)" }
      Say "Installed to $dest"
    } finally {
      Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
    }

    if ($OnWindows -and $env:ZEBRACAT_NO_MODIFY_PATH -ne '1') {
      $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
      $parts = @($userPath -split ';' | Where-Object { $_ })
      if ($parts -notcontains $binDir) {
        [Environment]::SetEnvironmentVariable('Path', (@($binDir) + $parts) -join ';', 'User')
        Say "Added $binDir to your PATH (open a new terminal to use it)."
      }
      if (($env:Path -split ';') -notcontains $binDir) { $env:Path = "$binDir;$env:Path" }
    }
    if ($OnWindows) {
      try { & $dest version } catch { Write-Host "warning: $dest did not start: $($_.Exception.Message)" -ForegroundColor Yellow }
    }
    Say "Run 'zebracat auth login' to get started."
  } catch {
    Write-Host "error: $($_.Exception.Message)" -ForegroundColor Red
    # Run as a file (powershell -File install.ps1): exit non-zero. Piped into
    # iex: don't close the person's window, just stop.
    if ($PSCommandPath) { exit 1 }
  }
}
