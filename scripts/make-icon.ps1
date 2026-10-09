<#
.SYNOPSIS
  Renders the BackupHarbor application icon (same geometry as frontend/logo.svg) and
  packs it into a multi-resolution Windows icon.

.DESCRIPTION
  Writes build/windows/icon.ico (16, 24, 32, 48, 64, 128, 256 px, PNG-compressed) and
  build/appicon.png (512 px). Existing files are only replaced with -Force.
#>
[CmdletBinding()]
param(
    [string]$OutDir = '',
    [switch]$Force
)
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Drawing
if (-not $OutDir) {
    $OutDir = Join-Path (Split-Path -Parent $MyInvocation.MyCommand.Path) '..\build'
}
$OutDir = [System.IO.Path]::GetFullPath($OutDir)

$icoPath = Join-Path $OutDir 'windows\icon.ico'
$pngPath = Join-Path $OutDir 'appicon.png'
if ((Test-Path $icoPath) -and -not $Force) {
    Write-Host "Icon already exists: $icoPath (use -Force to regenerate)"
    return
}
New-Item -ItemType Directory -Force (Split-Path $icoPath) | Out-Null

function New-IconBitmap([int]$size) {
    $s = $size / 128.0
    $bmp = New-Object System.Drawing.Bitmap $size, $size, ([System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
    $g = [System.Drawing.Graphics]::FromImage($bmp)
    $g.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::AntiAlias
    $g.PixelOffsetMode = [System.Drawing.Drawing2D.PixelOffsetMode]::HighQuality
    $g.Clear([System.Drawing.Color]::Transparent)

    # Rounded square with the brand gradient.
    $r = 28 * $s
    $path = New-Object System.Drawing.Drawing2D.GraphicsPath
    $d = 2 * $r
    $path.AddArc(0, 0, $d, $d, 180, 90)
    $path.AddArc($size - $d, 0, $d, $d, 270, 90)
    $path.AddArc($size - $d, $size - $d, $d, $d, 0, 90)
    $path.AddArc(0, $size - $d, $d, $d, 90, 90)
    $path.CloseFigure()
    $rect = New-Object System.Drawing.RectangleF 0, 0, $size, $size
    $brush = New-Object System.Drawing.Drawing2D.LinearGradientBrush $rect, ([System.Drawing.Color]::FromArgb(255, 20, 163, 149)), ([System.Drawing.Color]::FromArgb(255, 11, 85, 80)), 45.0
    $g.FillPath($brush, $path)

    $white = [System.Drawing.Color]::White
    # Restore arrow: circle of radius 36 around the centre, opening at the upper left.
    $pen = New-Object System.Drawing.Pen $white, (9 * $s)
    $pen.StartCap = [System.Drawing.Drawing2D.LineCap]::Round
    $pen.EndCap = [System.Drawing.Drawing2D.LineCap]::Round
    $g.DrawArc($pen, 28 * $s, 28 * $s, 72 * $s, 72 * $s, 270, 292.5)
    $head = [System.Drawing.PointF[]]@(
        (New-Object System.Drawing.PointF (20.5 * $s), (46 * $s)),
        (New-Object System.Drawing.PointF (40.9 * $s), (54.4 * $s)),
        (New-Object System.Drawing.PointF (37.6 * $s), (33.6 * $s)))
    $g.FillPolygon((New-Object System.Drawing.SolidBrush $white), $head)
    $hp = New-Object System.Drawing.Pen $white, (3 * $s)
    $hp.LineJoin = [System.Drawing.Drawing2D.LineJoin]::Round
    $g.DrawPolygon($hp, $head)

    # Check mark.
    $cp = New-Object System.Drawing.Pen $white, (10 * $s)
    $cp.StartCap = [System.Drawing.Drawing2D.LineCap]::Round
    $cp.EndCap = [System.Drawing.Drawing2D.LineCap]::Round
    $cp.LineJoin = [System.Drawing.Drawing2D.LineJoin]::Round
    $pts = [System.Drawing.PointF[]]@(
        (New-Object System.Drawing.PointF (46 * $s), (66 * $s)),
        (New-Object System.Drawing.PointF (59 * $s), (79 * $s)),
        (New-Object System.Drawing.PointF (84 * $s), (51 * $s)))
    $g.DrawLines($cp, $pts)
    $g.Dispose()
    return $bmp
}

function Get-PngBytes($bmp) {
    $ms = New-Object System.IO.MemoryStream
    $bmp.Save($ms, [System.Drawing.Imaging.ImageFormat]::Png)
    return ,$ms.ToArray()
}

$sizes = 16, 24, 32, 48, 64, 128, 256
$images = foreach ($sz in $sizes) {
    $b = New-IconBitmap $sz
    [pscustomobject]@{ Size = $sz; Data = (Get-PngBytes $b) }
    $b.Dispose()
}

# ICO container: header, directory, PNG payloads.
$ms = New-Object System.IO.MemoryStream
$w = New-Object System.IO.BinaryWriter $ms
$w.Write([uint16]0); $w.Write([uint16]1); $w.Write([uint16]$images.Count)
$offset = 6 + 16 * $images.Count
foreach ($img in $images) {
    $dim = if ($img.Size -ge 256) { 0 } else { $img.Size }
    $w.Write([byte]$dim); $w.Write([byte]$dim); $w.Write([byte]0); $w.Write([byte]0)
    $w.Write([uint16]1); $w.Write([uint16]32)
    $w.Write([uint32]([byte[]]$img.Data).Length); $w.Write([uint32]$offset)
    $offset += ([byte[]]$img.Data).Length
}
foreach ($img in $images) { $w.Write([byte[]]$img.Data) }
$w.Flush()
[System.IO.File]::WriteAllBytes($icoPath, $ms.ToArray())

$big = New-IconBitmap 512
[System.IO.File]::WriteAllBytes($pngPath, (Get-PngBytes $big))
$big.Dispose()
Write-Host "Wrote $icoPath ($($images.Count) sizes) and $pngPath"
