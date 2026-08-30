"""Minimal glyph subsetter for colour-font test fixtures.

Keeps a handful of codepoints and every glyph they reach (COLR layers, composite
components), renumbers nothing -- glyph IDs are PRESERVED and unused glyphs are
replaced by empty outlines. That keeps cmap/COLR/hmtx index-compatible with the
original, which is what makes this ~100 lines instead of a real subsetter.
"""
import struct, sys

def tables(d):
    nt = struct.unpack('>H', d[4:6])[0]
    t = {}
    for i in range(nt):
        r = 12+16*i
        tag = d[r:r+4].decode('latin1')
        cks, off, ln = struct.unpack('>III', d[r+4:r+16])
        t[tag] = (cks, off, ln)
    return t

def tdata(d, t, tag):
    if tag not in t: return None
    _, o, l = t[tag]
    return d[o:o+l]

def parse_cmap(b):
    """codepoint -> gid, from a format 4 or 12 subtable."""
    n = struct.unpack('>H', b[2:4])[0]
    best = None
    for i in range(n):
        pid, eid, off = struct.unpack('>HHI', b[4+8*i:12+8*i])
        fmt = struct.unpack('>H', b[off:off+2])[0]
        if fmt in (4, 12):
            best = (fmt, off) if best is None or fmt == 12 else best
    if not best: return {}
    fmt, off = best
    m = {}
    if fmt == 12:
        ngroups = struct.unpack('>I', b[off+12:off+16])[0]
        for g in range(ngroups):
            s, e, gid = struct.unpack('>III', b[off+16+12*g:off+28+12*g])
            for c in range(s, min(e, s+2000)+1):
                m[c] = gid + (c - s)
    else:
        segX2 = struct.unpack('>H', b[off+6:off+8])[0]
        seg = segX2//2
        ends = struct.unpack('>%dH'%seg, b[off+14:off+14+segX2])
        starts = struct.unpack('>%dH'%seg, b[off+16+segX2:off+16+2*segX2])
        deltas = struct.unpack('>%dh'%seg, b[off+16+2*segX2:off+16+3*segX2])
        rngOff = off+16+3*segX2
        rngs = struct.unpack('>%dH'%seg, b[rngOff:rngOff+segX2])
        for i in range(seg):
            for c in range(starts[i], min(ends[i], starts[i]+2000)+1):
                if c == 0xFFFF: continue
                if rngs[i] == 0:
                    m[c] = (c + deltas[i]) & 0xFFFF
                else:
                    gi = rngOff + 2*i + rngs[i] + 2*(c - starts[i])
                    if gi+2 <= len(b):
                        g = struct.unpack('>H', b[gi:gi+2])[0]
                        if g: m[c] = (g + deltas[i]) & 0xFFFF
    return m

def colr_deps(colr, gids):
    """Glyphs referenced as COLR layers of `gids` (v0 only; v1 handled by keeping all
    layer glyphs it can reach through the LayerList)."""
    if not colr: return set()
    ver = struct.unpack('>H', colr[0:2])[0]
    out = set()
    nb, boff, loff, nl = struct.unpack('>HIIH', colr[2:14])
    for i in range(nb):
        g, first, cnt = struct.unpack('>HHH', colr[boff+6*i:boff+6*i+6])
        if g in gids:
            for j in range(first, first+cnt):
                lg, _ = struct.unpack('>HH', colr[loff+4*j:loff+4*j+4])
                out.add(lg)
    if ver >= 1:
        b1, l1 = struct.unpack('>II', colr[14:22])
        if b1:
            n1 = struct.unpack('>I', colr[b1:b1+4])[0]
            for i in range(n1):
                gid, poff = struct.unpack('>HI', colr[b1+4+6*i:b1+4+6*i+6])
                if gid in gids:
                    walk_paint(colr, b1+poff, l1, out, set())
    return out

def u24(b, o):
    return (b[o] << 16) | (b[o+1] << 8) | b[o+2]

def walk_paint(colr, off, layerlist, out, seen, depth=0):
    """Collect glyph ids referenced by a COLRv1 paint subgraph."""
    if off in seen or depth > 64 or off + 1 > len(colr): return
    seen.add(off)
    fmt = colr[off]
    if fmt == 1:  # PaintColrLayers: numLayers(u8) firstLayerIndex(u32) -> LayerList
        n = colr[off+1]
        first = struct.unpack('>I', colr[off+2:off+6])[0]
        if layerlist:
            cnt = struct.unpack('>I', colr[layerlist:layerlist+4])[0]
            for j in range(first, min(first+n, cnt)):
                po = struct.unpack('>I', colr[layerlist+4+4*j:layerlist+8+4*j])[0]
                walk_paint(colr, layerlist+po, layerlist, out, seen, depth+1)
    elif fmt == 10 or fmt == 11:  # PaintGlyph: paintOffset(u24) glyphID(u16)
        po = u24(colr, off+1)
        gid = struct.unpack('>H', colr[off+4:off+6])[0]
        out.add(gid)
        if po: walk_paint(colr, off+po, layerlist, out, seen, depth+1)
    elif fmt == 12 or fmt == 13:  # PaintColrGlyph / transforms: paintOffset(u24) ...
        po = u24(colr, off+1)
        if po: walk_paint(colr, off+po, layerlist, out, seen, depth+1)
    elif 14 <= fmt <= 31:  # transform/composite paints begin with a paint offset
        po = u24(colr, off+1)
        if po: walk_paint(colr, off+po, layerlist, out, seen, depth+1)

def glyf_deps(glyf, loca, gid, long_loca):
    """Composite components of gid."""
    def bounds(g):
        if long_loca:
            return struct.unpack('>I', loca[4*g:4*g+4])[0], struct.unpack('>I', loca[4*g+4:4*g+8])[0]
        a = struct.unpack('>H', loca[2*g:2*g+2])[0]*2
        b = struct.unpack('>H', loca[2*g+2:2*g+4])[0]*2
        return a, b
    s, e = bounds(gid)
    if e <= s: return set()
    d = glyf[s:e]
    nc = struct.unpack('>h', d[0:2])[0]
    if nc >= 0: return set()
    out, p = set(), 10
    while True:
        flags, gi = struct.unpack('>HH', d[p:p+4]); p += 4
        out.add(gi)
        p += 4 if (flags & 1) else 2
        if flags & 8: p += 2
        elif flags & 0x40: p += 4
        elif flags & 0x80: p += 8
        if not (flags & 0x20): break
    return out

def build(order, tabs):
    n = len(order); sr = 1
    while sr*2 <= n: sr *= 2
    head = struct.pack('>IHHHH', 0x00010000, n, sr*16, sr.bit_length()-1, n*16-sr*16)
    off = 12+16*n; recs=[]; body=[]
    for tag in order:
        data = tabs[tag]
        pad = (-len(data)) % 4
        recs.append(struct.pack('>4sIII', tag.encode('latin1'), 0, off, len(data)))
        body.append(data+b'\x00'*pad); off += len(data)+pad
    return head+b''.join(recs)+b''.join(body)

def subset(path, out, keep_cps, drop_extra=()):
    d = open(path,'rb').read()
    t = tables(d)
    cmap = parse_cmap(tdata(d,t,'cmap'))
    keep = {0}
    for c in keep_cps:
        if c in cmap: keep.add(cmap[c])
    if not keep - {0}:
        print('  !! none of the requested codepoints are in', path); return
    glyf, loca = tdata(d,t,'glyf'), tdata(d,t,'loca')
    head = tdata(d,t,'head')
    long_loca = struct.unpack('>h', head[50:52])[0] == 1 if head else False
    colr = tdata(d,t,'COLR')
    dep = colr_deps(colr, keep)
    keep_all_glyf = -1 in dep
    keep |= {g for g in dep if g >= 0}
    if glyf and loca and not keep_all_glyf:
        seen=set()
        stack=list(keep)
        while stack:
            g=stack.pop()
            if g in seen: continue
            seen.add(g)
            for c in glyf_deps(glyf, loca, g, long_loca): stack.append(c)
        keep |= seen
    newtabs = {}
    for tag,(cks,o,l) in t.items():
        if tag in drop_extra: continue
        newtabs[tag] = d[o:o+l]
    if glyf and loca and not keep_all_glyf:
        nglyphs = (len(loca)//4 - 1) if long_loca else (len(loca)//2 - 1)
        newglyf=b''; newloca=[]
        for g in range(nglyphs):
            newloca.append(len(newglyf))
            if g in keep:
                if long_loca:
                    s=struct.unpack('>I',loca[4*g:4*g+4])[0]; e=struct.unpack('>I',loca[4*g+4:4*g+8])[0]
                else:
                    s=struct.unpack('>H',loca[2*g:2*g+2])[0]*2; e=struct.unpack('>H',loca[2*g+2:2*g+4])[0]*2
                blob = glyf[s:e]
                newglyf += blob + b'\x00'*((-len(blob))%4)
        newloca.append(len(newglyf))
        newtabs['glyf']=newglyf
        if long_loca:
            newtabs['loca']=b''.join(struct.pack('>I',x) for x in newloca)
        else:
            newtabs['loca']=b''.join(struct.pack('>H',x//2) for x in newloca)
    order=sorted(newtabs)
    open(out,'wb').write(build(order,newtabs))
    print('  %-28s %6.2f -> %6.2f MB  keptGlyphs=%d' % (out, len(d)/1048576, len(open(out,'rb').read())/1048576, len(keep)))

if __name__=='__main__':
    pass
CPS = [0x1F600, 0x1F389, 0x2764, 0x1F44D, 0x1F31F]  # grin, party popper, heart, thumbs up, star
DROP = {'GSUB','GPOS','GDEF','DSIG','vhea','vmtx','morx','feat','trak','meta','bgcl','cntr','OS/2','post'}
#subset('Twemoji.Mozilla.ttf','Twemoji-COLRv0.ttf', CPS, DROP)
#subset('noto-colr.ttf','NotoColorEmoji-COLRv1.ttf', CPS, DROP)
