// Draws Voir's app icon: an eye whose lids are a branch that splits from
// one commit and merges into another, with the iris as the commit between.
// Only the mark is drawn, on a transparent canvas, with the holes of the
// nodes and the pupil cut out: it is the layer of the Icon Composer icon,
// whose background follows the light or dark appearance.
//
//   swift tools/appicon/icon.swift "tools/appicon/AppIcon.icon/Assets/icon 2.png"
import AppKit
import CoreGraphics

let size = 1024
let out = CommandLine.arguments.count > 1 ? CommandLine.arguments[1] : "icon.png"

func rgb(_ hex: UInt32, _ a: CGFloat = 1) -> CGColor {
    CGColor(srgbRed: CGFloat((hex >> 16) & 0xff) / 255,
            green: CGFloat((hex >> 8) & 0xff) / 255,
            blue: CGFloat(hex & 0xff) / 255, alpha: a)
}

let space = CGColorSpace(name: CGColorSpace.sRGB)!
let ctx = CGContext(data: nil, width: size, height: size, bitsPerComponent: 8, bytesPerRow: 0,
                    space: space, bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!
// Work top-down like a design tool.
ctx.translateBy(x: 0, y: CGFloat(size))
ctx.scaleBy(x: 1, y: -1)

func gradient(_ stops: [(CGFloat, CGColor)]) -> CGGradient {
    CGGradient(colorsSpace: space, colors: stops.map { $0.1 } as CFArray,
               locations: stops.map { $0.0 })!
}

// Punches a hole through what is drawn, so the background shows through.
func clearEllipse(_ c: CGPoint, _ r: CGFloat) {
    ctx.saveGState()
    ctx.setBlendMode(.clear)
    ctx.fillEllipse(in: CGRect(x: c.x - r, y: c.y - r, width: 2 * r, height: 2 * r))
    ctx.restoreGState()
}

let mid: CGFloat = 512
let left = CGPoint(x: 222, y: mid), right = CGPoint(x: 802, y: mid)
let lift: CGFloat = 245
let lineWidth: CGFloat = 36

let lanes = CGMutablePath()
// Upper and lower lids: the branch and the line it merges back into.
lanes.move(to: left)
lanes.addCurve(to: right, control1: CGPoint(x: 380, y: mid - lift), control2: CGPoint(x: 644, y: mid - lift))
lanes.move(to: left)
lanes.addCurve(to: right, control1: CGPoint(x: 380, y: mid + lift), control2: CGPoint(x: 644, y: mid + lift))

let laneColors = gradient([(0, rgb(0xFFE066)), (0.5, rgb(0xFFB81C)), (1, rgb(0xFF8A00))])

func strokeGradient(_ path: CGPath, width: CGFloat, glow: CGColor?) {
    let stroked = path.copy(strokingWithWidth: width, lineCap: .round, lineJoin: .round, miterLimit: 10)
    if let glow {
        ctx.saveGState()
        ctx.setShadow(offset: .zero, blur: 40, color: glow)
        ctx.addPath(stroked)
        ctx.setFillColor(rgb(0xFFB81C, 0.5))
        ctx.fillPath()
        ctx.restoreGState()
    }
    ctx.saveGState()
    ctx.addPath(stroked)
    ctx.clip()
    ctx.drawLinearGradient(laneColors, start: CGPoint(x: left.x - 40, y: 0), end: CGPoint(x: right.x + 40, y: 0), options: [])
    ctx.restoreGState()
}

strokeGradient(lanes, width: lineWidth, glow: rgb(0xFFA000, 0.45))

// Commit nodes where the branch splits and merges.
func node(_ c: CGPoint, color: CGColor) {
    let r: CGFloat = 40
    ctx.saveGState()
    ctx.setShadow(offset: .zero, blur: 24, color: color)
    ctx.setFillColor(color)
    ctx.fillEllipse(in: CGRect(x: c.x - r, y: c.y - r, width: 2 * r, height: 2 * r))
    ctx.restoreGState()
    clearEllipse(c, 19)
}
node(left, color: rgb(0xFFE066))
node(right, color: rgb(0xFF8A00))

// Iris: the commit being looked at.
let eye = CGPoint(x: 512, y: mid)
let iris: CGFloat = 112
ctx.saveGState()
ctx.setShadow(offset: .zero, blur: 50, color: rgb(0xFFA000, 0.55))
ctx.addEllipse(in: CGRect(x: eye.x - iris, y: eye.y - iris, width: 2 * iris, height: 2 * iris))
ctx.setFillColor(rgb(0xFFB81C))
ctx.fillPath()
ctx.restoreGState()
ctx.saveGState()
ctx.addEllipse(in: CGRect(x: eye.x - iris, y: eye.y - iris, width: 2 * iris, height: 2 * iris))
ctx.clip()
ctx.drawLinearGradient(laneColors, start: CGPoint(x: eye.x - iris, y: eye.y - iris),
                       end: CGPoint(x: eye.x + iris, y: eye.y + iris), options: [])
ctx.drawRadialGradient(gradient([(0, rgb(0xFFFFFF, 0.0)), (0.75, rgb(0xFFFFFF, 0.0)), (1, rgb(0x7A3A00, 0.45))]),
                       startCenter: eye, startRadius: 0, endCenter: eye, endRadius: iris, options: [])
ctx.restoreGState()

// Pupil and catch light.
clearEllipse(eye, 50)
let glint = CGPoint(x: eye.x + 34, y: eye.y - 36)
ctx.setFillColor(rgb(0xFFF6D6, 0.95))
ctx.fillEllipse(in: CGRect(x: glint.x - 17, y: glint.y - 17, width: 34, height: 34))

let image = ctx.makeImage()!
let rep = NSBitmapImageRep(cgImage: image)
try! rep.representation(using: .png, properties: [:])!.write(to: URL(fileURLWithPath: out))
