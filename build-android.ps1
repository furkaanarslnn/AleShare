<#
Builds AleShare: the Go core into android\app\libs\wgcore.aar (gomobile) and then the APK.

Needs Go, JDK 17 (Temurin) and the Android SDK with an NDK under %LOCALAPPDATA%\Android\Sdk. Usage:
  .\build-android.ps1 [-SkipCore]   debug APK, for development
  .\build-android.ps1 -Release      release APK and the Windows program, as dist\AleShare.apk and dist\AleShare.exe,
                                    copied to uygulama\; the program is signed with
                                    android\keystore\release-ed25519.key and travels inside the APK
  .\build-android.ps1 -Release -SameRelease   the same, but the release keeps its number and name: for changing what
                                    a release holds without counting on
A release first runs all tests (Go, then the app's unit tests); a failing test stops it before anything is
copied. -SkipTests leaves them out, for a quick try only.
#>
param([switch]$SkipCore, [switch]$Release, [switch]$SkipTests, [switch]$SameRelease)
$ErrorActionPreference = 'Stop'
$runTests = $Release -and -not $SkipTests

$root = $PSScriptRoot
$sdk = Join-Path $env:LOCALAPPDATA 'Android\Sdk'
$ndk = (Get-ChildItem "$sdk\ndk" -Directory | Sort-Object Name | Select-Object -Last 1).FullName
$jdk = (Get-ChildItem 'C:\Program Files\Eclipse Adoptium' -Directory -Filter 'jdk-17*' | Select-Object -First 1).FullName

$env:ANDROID_HOME = $sdk
$env:ANDROID_NDK_HOME = $ndk
$env:JAVA_HOME = $jdk
$env:Path = "$jdk\bin;$env:USERPROFILE\go\bin;" + [Environment]::GetEnvironmentVariable('Path', 'Machine') + ';' + [Environment]::GetEnvironmentVariable('Path', 'User')

if ($runTests) {
  Push-Location "$root\core"
  try {
    go test ./...
    if ($LASTEXITCODE) { throw "Go tests failed ($LASTEXITCODE): no release" }
  } finally {
    Pop-Location
  }
}

# The app is built through a temporary drive letter that stands for this folder. The folder's real path must not
# end up inside the app: gomobile builds the core as a module replaced by the folder it lies in, and Go writes that
# place into the library ("hotspot/core => C:\Users\<name>\...\core"), -trimpath or not. Through the drive it reads
# "H:\core". The Android Gradle plugin also refuses project paths with non-ASCII characters.
$used = (Get-PSDrive -PSProvider FileSystem).Name
$letter = 'H', 'I', 'J', 'K', 'L', 'M', 'N', 'O', 'P', 'Q', 'R', 'S', 'T', 'U', 'V', 'W' | Where-Object { $used -notcontains $_ } | Select-Object -First 1
subst "${letter}:" $root
$script:drive = "${letter}:"
$androidDir = "$script:drive\android"
trap {
  if ($script:drive) { subst $script:drive /d | Out-Null; $script:drive = $null }
  break
}

if (-not $SkipCore) {
  New-Item -ItemType Directory -Force "$root\android\app\libs" | Out-Null
  Push-Location "$script:drive\core"
  try {
    gomobile bind -target=android/arm64 -androidapi 26 -javapkg dev.hotspot -trimpath -ldflags '-s -w' -o "$script:drive\android\app\libs\wgcore.aar" ./wgcore
    if ($LASTEXITCODE) { throw "gomobile bind failed ($LASTEXITCODE)" }
  } finally {
    Pop-Location
  }
}

# Java ignores HTTP(S)_PROXY. If a proxy is configured, hand it to Gradle for this run only; nothing is saved.
$proxy = if ($env:HTTPS_PROXY) { $env:HTTPS_PROXY } else { $env:HTTP_PROXY }
if ($proxy) {
  $u = [Uri]$proxy
  $env:GRADLE_OPTS = "-Dhttp.proxyHost=$($u.Host) -Dhttp.proxyPort=$($u.Port) -Dhttps.proxyHost=$($u.Host) -Dhttps.proxyPort=$($u.Port)"
}

# Every release gets the next number, so it installs over the one before it on every device.
$versionFile = "$root\android\version.properties"
if ($Release) {
  $text = Get-Content $versionFile -Raw
  if ($text -notmatch '(?m)^code=(\d+)') { throw "no code= line in $versionFile" }
  $code = [int]$Matches[1]
  if (-not $SameRelease) { $code++ }
  # What the release is called follows from its number; the Go core knows the rule.
  Push-Location "$root\core"
  try { $name = "$(go run ./cmd/hotspot-sign name $code)".Trim() } finally { Pop-Location }
  if ($LASTEXITCODE -or $name -notmatch '^\d+(\.\d+)+$') { throw "release $code has no name" }
  $utf8 = New-Object Text.UTF8Encoding($false)
  [IO.File]::WriteAllText($versionFile, (($text -replace '(?m)^code=\d+', "code=$code") -replace '(?m)^name=\S+', "name=$name"), $utf8)
  Write-Host "Release $name"
}

# What a user needs, in one place.
$dist = "$root\dist"
$apps = "$root\uygulama"
if ($Release) {
  New-Item -ItemType Directory -Force $dist | Out-Null
  # The Windows program first: it travels inside the APK, so that a phone can hand it to the computers.
  Push-Location "$root\core\cmd\hotspot-pc"
  try {
    # The program's icon, manifest (sharp at any display scale) and version, as a resource the build links in.
    go run github.com/tc-hib/go-winres@v0.3.3 make --in winres/winres.json --out rsrc --arch amd64 --file-version $name --product-version $name
    if ($LASTEXITCODE) { throw "go-winres failed ($LASTEXITCODE)" }
    $env:GOOS = 'windows'; $env:GOARCH = 'amd64'
    # A windowed program (no black console behind the window); the command line still gets its output.
    go build -trimpath -ldflags "-s -w -H=windowsgui -X main.version=$name" -o "$dist\AleShare.exe" .
    if ($LASTEXITCODE) { throw "go build AleShare.exe failed ($LASTEXITCODE)" }
  } finally {
    Remove-Item Env:GOOS, Env:GOARCH -ErrorAction SilentlyContinue
    Pop-Location
  }
  # The release signature: computers install a program that came from a phone only with it (internal/release).
  $releaseKey = "$root\android\keystore\release-ed25519.key"
  if (-not (Test-Path $releaseKey)) { throw "no release key at $releaseKey (restore it from your offline backup of the signing keys)" }
  Push-Location "$root\core"
  try {
    go run ./cmd/hotspot-sign sign -key $releaseKey -code $code "$dist\AleShare.exe"
    if ($LASTEXITCODE) { throw "signing AleShare.exe failed ($LASTEXITCODE)" }
  } finally {
    Pop-Location
  }
  $assets = "$root\android\app\src\main\assets"
  New-Item -ItemType Directory -Force $assets | Out-Null
  # Inside the APK the program keeps its old name: phones of every release ask for it by that name.
  Copy-Item "$dist\AleShare.exe" "$assets\hotspot-pc.exe" -Force
  Remove-Item "$dist\hotspot-pc.exe" -Force -ErrorAction SilentlyContinue # the name before 1.0.32
}

$task = if ($Release) { 'assembleRelease' } else { 'assembleDebug' }
# The app's unit tests run before the APK is put together; Gradle stops at the first failure. They run on the
# debug variant: the Android Gradle plugin makes unit tests for that build type only.
# @(...) around the whole if: a one-element answer would otherwise become a string, splatted letter by letter.
$tasks = @(if ($runTests) { 'testDebugUnitTest', $task } else { $task })
Push-Location $androidDir
try {
  & .\gradlew.bat @tasks --console=plain
  if ($LASTEXITCODE) { throw "gradle failed ($LASTEXITCODE)" }
} finally {
  Pop-Location
  if ($script:drive) { subst $script:drive /d | Out-Null; $script:drive = $null }
}

if (-not $Release) {
  Write-Host "APK: $root\android\app\build\outputs\apk\debug\app-debug.apk"
  return
}

Copy-Item "$root\android\app\build\outputs\apk\release\app-release.apk" "$dist\AleShare.apk" -Force
# The two files are always called AleShare.apk and AleShare.exe, whatever the release. What older builds left under
# other names goes.
Get-ChildItem "$dist\AleShare-*.apk", "$dist\Hotspot*.apk", "$dist\aleshare-pc.exe" -ErrorAction SilentlyContinue | Remove-Item -Force
# The folder the user installs from: always just the newest APK, and the Windows program under the name the app
# and the guide use (setting it up there keeps the APK beside it too, for the phones).
New-Item -ItemType Directory -Force $apps | Out-Null
Get-ChildItem "$apps\AleShare*.apk", "$apps\Hotspot*.apk" -ErrorAction SilentlyContinue | Remove-Item -Force
Copy-Item "$dist\AleShare.apk" "$apps\AleShare.apk" -Force
Write-Host "uygulama\AleShare.apk"
# The program there may be running (its window open): Windows refuses to overwrite it but lets it be moved away.
# The same goes for the program under the names it had before, which must not stay beside the new one.
foreach ($old in 'AleShare.exe', 'aleshare-pc.exe', 'hotspot-pc.exe') {
  if (Test-Path "$apps\$old") {
    try {
      Remove-Item "$apps\$old" -Force -ErrorAction Stop
    } catch {
      Move-Item "$apps\$old" (Join-Path $env:TEMP ("{0}-{1}.exe" -f [IO.Path]::GetFileNameWithoutExtension($old), (Get-Date -Format 'yyyyMMddHHmmss')))
    }
  }
}
Copy-Item "$dist\AleShare.exe" "$apps\AleShare.exe" -Force
Write-Host "uygulama\AleShare.exe"
Get-ChildItem $dist | ForEach-Object { Write-Host ("dist\{0}  {1:N1} MB  SHA-256 {2}" -f $_.Name, ($_.Length / 1MB), (Get-FileHash $_.FullName -Algorithm SHA256).Hash) }
