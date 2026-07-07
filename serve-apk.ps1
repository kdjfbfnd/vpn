$ErrorActionPreference = "Stop"

$port = 8000
$apkPath = "C:\Users\Administrator\Desktop\VPN\app\build\outputs\apk\debug\app-debug.apk"

if (-not (Test-Path -LiteralPath $apkPath)) {
    throw "APK not found: $apkPath"
}

$listener = [System.Net.HttpListener]::new()
$listener.Prefixes.Add("http://localhost:$port/")
$listener.Start()

try {
    while ($listener.IsListening) {
        $context = $listener.GetContext()
        $request = $context.Request
        $response = $context.Response

        if ($request.Url.AbsolutePath -eq "/" -or $request.Url.AbsolutePath -eq "/app-debug.apk") {
            $bytes = [System.IO.File]::ReadAllBytes($apkPath)
            $response.StatusCode = 200
            $response.ContentType = "application/vnd.android.package-archive"
            $response.AddHeader("Content-Disposition", "attachment; filename=app-debug.apk")
            $response.ContentLength64 = $bytes.Length
            $response.OutputStream.Write($bytes, 0, $bytes.Length)
        } else {
            $response.StatusCode = 404
        }

        $response.OutputStream.Close()
    }
} finally {
    $listener.Stop()
    $listener.Close()
}
