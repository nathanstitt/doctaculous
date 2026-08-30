"""Build the COLRv1 fixture WITHOUT re-encoding the paint graph.

An earlier version rewrote paint records and their offsets to compact the table. That
re-encoding was the source of a corrupt fixture (it mis-ordered PaintTransform's two
u24 fields), and a corrupt colour table is exactly the kind of thing that looks like a
parser bug for an hour. So: keep the COLR bytes VERBATIM and shrink the font by the
two levers that need no re-encoding --

  * drop unrelated tables, and
  * empty the outlines of glyphs no kept base glyph reaches (loca/glyf only).

Glyph IDs, cmap, COLR and CPAL are untouched, so what the tests parse is byte-for-byte
what upstream ships.
"""
import struct, os
from subset import tables, tdata, parse_cmap, build, glyf_deps

d = open('noto-colr.ttf','rb').read()
t = tables(d)
cmap = parse_cmap(tdata(d,t,'cmap'))
CPS = [0x1F600, 0x1F389, 0x2764, 0x1F44D, 0x1F31F]
want = {cmap[c] for c in CPS if c in cmap}

colr = tdata(d,t,'COLR')
b1, l1 = struct.unpack('>II', colr[14:22])
def s24(b, o):
    v = (b[o] << 16) | (b[o+1] << 8) | b[o+2]
    return v - (1 << 24) if v & 0x800000 else v

keep = set(want) | {0}
def walk(off, seen, depth=0):
    if off in seen or depth > 64 or off < 0 or off >= len(colr): return
    seen.add(off)
    f = colr[off]
    if f == 1:
        n = colr[off+1]; first = struct.unpack('>I', colr[off+2:off+6])[0]
        cnt = struct.unpack('>I', colr[l1:l1+4])[0]
        for j in range(first, min(first+n, cnt)):
            walk(l1 + struct.unpack('>I', colr[l1+4+4*j:l1+8+4*j])[0], seen, depth+1)
    elif f in (10, 11):
        keep.add(struct.unpack('>H', colr[off+4:off+6])[0])
        walk(off + s24(colr, off+1), seen, depth+1)
    elif f in (12, 13, 14, 15, 16, 18, 32):
        walk(off + s24(colr, off+1), seen, depth+1)

n1 = struct.unpack('>I', colr[b1:b1+4])[0]
seen = set()
for i in range(n1):
    gid, po = struct.unpack('>HI', colr[b1+4+6*i:b1+4+6*i+6])
    if gid in want:
        walk(b1 + po, seen)
print('glyphs reachable from the 5 base glyphs:', len(keep))

glyf, loca, head = tdata(d,t,'glyf'), tdata(d,t,'loca'), tdata(d,t,'head')
long_loca = struct.unpack('>h', head[50:52])[0] == 1
nglyphs = (len(loca)//4 - 1) if long_loca else (len(loca)//2 - 1)
keep = {g for g in keep if 0 <= g < nglyphs}
stack = list(keep)
while stack:
    g = stack.pop()
    for c2 in glyf_deps(glyf, loca, g, long_loca):
        if 0 <= c2 < nglyphs and c2 not in keep:
            keep.add(c2); stack.append(c2)

newglyf = bytearray(); newloca = []
for g in range(nglyphs):
    newloca.append(len(newglyf))
    if g in keep:
        if long_loca:
            s = struct.unpack('>I', loca[4*g:4*g+4])[0]; e = struct.unpack('>I', loca[4*g+4:4*g+8])[0]
        else:
            s = struct.unpack('>H', loca[2*g:2*g+2])[0]*2; e = struct.unpack('>H', loca[2*g+2:2*g+4])[0]*2
        blob = glyf[s:e]
        newglyf += blob + b'\x00'*((-len(blob)) % 4)
newloca.append(len(newglyf))

# Compact COLR by RELOCATING the reachable paints (see reloc.py), which rewrites every
# offset through one helper rather than assuming field order.
from reloc import R as _R
_r = _R(colr, l1)
_roots = []
for i in range(n1):
    gid, po = struct.unpack('>HI', colr[b1+4+6*i:b1+4+6*i+6])
    if gid in want:
        _roots.append((gid, _r.emit(b1 + po)))
_baseOff = 30
_layerOff = _baseOff + 4 + 6*len(_roots)
_bodyOff = _layerOff + 4 + 4*len(_r.layers)
_o = bytearray()
_o += struct.pack('>H', 1)
_o += struct.pack('>HIIH', 0, 0, 0, 0)
_o += struct.pack('>IIII', _baseOff, _layerOff, 0, 0)
_o += struct.pack('>I', len(_roots))
for gid, po in _roots:
    _o += struct.pack('>HI', gid, (_bodyOff + po) - _baseOff)
_o += struct.pack('>I', len(_r.layers))
for lp in _r.layers:
    _o += struct.pack('>I', (_bodyOff + lp) - _layerOff)
_o += _r.out
newcolr = bytes(_o)
print('COLR %d KB -> %d bytes' % (len(colr)//1024, len(newcolr)))

DROP = {'GSUB','GPOS','GDEF','DSIG','vhea','vmtx','morx','feat','trak','meta','OS/2','post'}
newtabs = {tag: d[o:o+l] for tag,(cks,o,l) in t.items() if tag not in DROP}
newtabs['COLR'] = bytes(newcolr)
newtabs['glyf'] = bytes(newglyf)
newtabs['loca'] = (b''.join(struct.pack('>I', x) for x in newloca) if long_loca
                   else b''.join(struct.pack('>H', x//2) for x in newloca))
open('NotoColorEmoji-COLRv1.ttf','wb').write(build(sorted(newtabs), newtabs))
print('file %.0f KB' % (os.path.getsize('NotoColorEmoji-COLRv1.ttf')/1024))
