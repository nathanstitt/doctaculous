"""Subset CBDT/CBLC to a few glyphs by rewriting the strike's index subtables.

Keeps the sfnt glyph numbering intact (like subset.py) and emits a single
IndexSubTable format 1 covering exactly the kept glyph range, so the bitmap data
shrinks from every emoji to the handful the tests use.
"""
import struct
from subset import tables, tdata, parse_cmap, build

d = open('NotoColorEmoji.ttf','rb').read()
t = tables(d)
cmap = parse_cmap(tdata(d,t,'cmap'))
CPS = [0x1F600, 0x1F389, 0x2764, 0x1F44D, 0x1F31F]
keep = sorted({cmap[c] for c in CPS if c in cmap})
print('keep gids', keep)

cblc = tdata(d,t,'CBLC'); cbdt = tdata(d,t,'CBDT')
numSizes = struct.unpack('>I', cblc[4:8])[0]
r = 8
ilso, ilss, numtab, _ = struct.unpack('>IIII', cblc[r:r+16])
# Collect (gid -> (offset,len,imageFormat)) by walking the existing index subtables.
loc = {}
for i in range(numtab):
    a = ilso + 8*i
    first, last, addOff = struct.unpack('>HHI', cblc[a:a+8])
    h = ilso + addOff
    idxFmt, imgFmt, imgDataOff = struct.unpack('>HHI', cblc[h:h+8])
    if idxFmt == 1:
        n = last-first+2
        offs = struct.unpack('>%dI'%n, cblc[h+8:h+8+4*n])
        for k, g in enumerate(range(first, last+1)):
            if offs[k+1] > offs[k]:
                loc[g] = (imgDataOff+offs[k], offs[k+1]-offs[k], imgFmt)
    elif idxFmt == 2:
        sz, = struct.unpack('>I', cblc[h+8:h+12])
        for k, g in enumerate(range(first, last+1)):
            loc[g] = (imgDataOff+sz*k, sz, imgFmt)
print('indexed glyphs', len(loc), 'of which kept', sum(1 for g in keep if g in loc))

kept = [g for g in keep if g in loc]
if not kept:
    raise SystemExit('none of the requested glyphs have bitmaps')
first, last = min(kept), max(kept)
newdata = bytearray(cbdt[:4])  # CBDT header (version)
offs = []
for g in range(first, last+1):
    offs.append(len(newdata))
    if g in loc and g in kept:
        o, l, _ = loc[g]
        newdata += cbdt[o:o+l]
offs.append(len(newdata))

imgFmt = loc[kept[0]][2]
sub = struct.pack('>HHI', 1, imgFmt, 4) + b''.join(struct.pack('>I', x-4) for x in offs)
arrays = struct.pack('>HHI', first, last, 8)     # one IndexSubTableArray record
newIlso = 8 + 48                                  # after header + 1 BitmapSize record
newCblc = bytearray(cblc[:8])
bs = bytearray(cblc[8:8+48])
struct.pack_into('>IIII', bs, 0, newIlso, len(arrays)+len(sub), 1, 0)
newCblc += bs + arrays + sub
print('CBDT %.1fMB -> %.1fMB' % (len(cbdt)/1048576, len(newdata)/1048576))

newtabs = {}
DROP = {'GSUB','GPOS','GDEF','DSIG','vhea','vmtx','morx','feat','trak','meta','OS/2','post'}
for tag,(cks,o,l) in t.items():
    if tag in DROP: continue
    newtabs[tag] = d[o:o+l]
newtabs['CBLC'] = bytes(newCblc)
newtabs['CBDT'] = bytes(newdata)
order = sorted(newtabs)
open('NotoColorEmoji-CBDT.ttf','wb').write(build(order, newtabs))
import os
print('  NotoColorEmoji-CBDT.ttf %.2f MB' % (os.path.getsize('NotoColorEmoji-CBDT.ttf')/1048576))
