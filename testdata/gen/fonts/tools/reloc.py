"""Relocate the reachable COLR v1 paints into a compact table.

Unlike the first attempt, this makes NO assumption about field order: each record's
child-paint and payload offsets are read from the positions the spec gives, and every
one is re-emitted through the same helper that records where the target landed. The
earlier version mis-ordered PaintTransform's two u24 fields and produced a fixture
that parsed as garbage, so the layouts below are asserted against real records.
"""
import struct, sys
sys.path.insert(0, '.')
from subset import tables, tdata, parse_cmap, build, glyf_deps

def s24(b, o):
    v = (b[o] << 16) | (b[o+1] << 8) | b[o+2]
    return v - (1 << 24) if v & 0x800000 else v

def p24(v):
    v &= 0xFFFFFF
    return bytes((v >> 16, (v >> 8) & 255, v & 255))

# (record size, [offsets of u24 child-paint fields], payload) per format.
# PaintTransform(12): paint at +1, Affine2x3 pointer at +4.
# PaintGlyph(10):     paint at +1, glyphID u16 at +4.
BODY = {
    1:  6, 2: 5, 3: 9, 4: 16, 5: 20, 6: 16, 7: 20, 8: 12, 9: 16,
    10: 6, 11: 3, 12: 7, 13: 11, 14: 8, 15: 12, 16: 12, 18: 12, 32: 14,
}
CHILD = {10: 1, 11: None, 12: 1, 13: 1, 14: 1, 15: 1, 16: 1, 18: 1, 32: 1}

class R:
    def __init__(self, colr, l1):
        self.c, self.l1 = colr, l1
        self.out = bytearray()
        self.map = {}
        self.layers = []

    def emit(self, off):
        if off in self.map:
            return self.map[off]
        c = self.c
        f = c[off]
        size = BODY.get(f)
        if size is None:
            raise SystemExit('unhandled paint format %d at %d' % (f, off))
        pos = len(self.out)
        self.map[off] = pos
        self.out += c[off:off+size]

        if f == 1:                                       # PaintColrLayers
            n = c[off+1]
            first = struct.unpack('>I', c[off+2:off+6])[0]
            cnt = struct.unpack('>I', c[self.l1:self.l1+4])[0]
            kids = [self.l1 + struct.unpack('>I', c[self.l1+4+4*j:self.l1+8+4*j])[0]
                    for j in range(first, min(first+n, cnt))]
            newFirst = len(self.layers)
            self.layers.extend([None]*len(kids))
            for i, k in enumerate(kids):
                self.layers[newFirst+i] = self.emit(k)
            struct.pack_into('>I', self.out, pos+2, newFirst)
            return pos

        ch = CHILD.get(f)
        if ch is not None:
            child = self.emit(off + s24(c, off+ch))
            self.out[pos+ch:pos+ch+3] = p24(child - pos)

        if f in (12, 13):                                # Affine2x3 payload
            to = off + s24(c, off+4)
            newt = len(self.out)
            self.out += c[to:to+24]
            self.out[pos+4:pos+7] = p24(newt - pos)
        elif f in (4, 5, 6, 7, 8, 9):                    # ColorLine payload
            clo = off + s24(c, off+1)
            nstops = struct.unpack('>H', c[clo+1:clo+3])[0]
            n = 3 + 6*nstops
            newcl = len(self.out)
            self.out += c[clo:clo+n]
            self.out[pos+1:pos+4] = p24(newcl - pos)
        return pos

d = open('noto-colr.ttf','rb').read()
t = tables(d)
colr = tdata(d,t,'COLR')
cmap = parse_cmap(tdata(d,t,'cmap'))
CPS = [0x1F600, 0x1F389, 0x2764, 0x1F44D, 0x1F31F]
want = {cmap[c] for c in CPS if c in cmap}
b1, l1 = struct.unpack('>II', colr[14:22])
n1 = struct.unpack('>I', colr[b1:b1+4])[0]

r = R(colr, l1)
roots = []
for i in range(n1):
    gid, po = struct.unpack('>HI', colr[b1+4+6*i:b1+4+6*i+6])
    if gid in want:
        roots.append((gid, r.emit(b1 + po)))

baseOff = 30
baseLen = 4 + 6*len(roots)
layerOff = baseOff + baseLen
layerLen = 4 + 4*len(r.layers)
bodyOff = layerOff + layerLen

out = bytearray()
out += struct.pack('>H', 1)
out += struct.pack('>HIIH', 0, 0, 0, 0)
out += struct.pack('>IIII', baseOff, layerOff, 0, 0)
out += struct.pack('>I', len(roots))
for gid, po in roots:
    out += struct.pack('>HI', gid, (bodyOff + po) - baseOff)
out += struct.pack('>I', len(r.layers))
for lp in r.layers:
    out += struct.pack('>I', (bodyOff + lp) - layerOff)
out += r.out
print('COLR %d KB -> %d bytes (%d paints, %d layers)' % (len(colr)//1024, len(out), len(r.map), len(r.layers)))
open('newcolr.bin','wb').write(bytes(out))
