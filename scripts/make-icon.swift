import AppKit
let path = CommandLine.arguments[1]
let size = 1024
let bitmap = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: size, pixelsHigh: size, bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false, colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
NSGraphicsContext.saveGraphicsState()
NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: bitmap)
let background = NSBezierPath(roundedRect: NSRect(x: 60, y: 60, width: 904, height: 904), xRadius: 220, yRadius: 220)
NSColor(calibratedRed: 0.0, green: 0.478, blue: 1.0, alpha: 1).setFill(); background.fill()
let heights: [CGFloat] = [150, 300, 480, 650, 440, 280, 130]
NSColor.white.setFill()
for (index, height) in heights.enumerated() {
    NSBezierPath(roundedRect: NSRect(x: 220 + CGFloat(index) * 87, y: (1024 - height) / 2, width: 48, height: height), xRadius: 24, yRadius: 24).fill()
}
NSGraphicsContext.restoreGraphicsState()
try bitmap.representation(using: .png, properties: [:])!.write(to: URL(fileURLWithPath: path))
