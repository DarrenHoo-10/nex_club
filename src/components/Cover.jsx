const PALETTES = [
  ['#3b6bff', '#22b8ff', '#c4a8ff'],
  ['#7c5cff', '#ff7ac6', '#7fe3ff'],
  ['#00b8a9', '#4ddcff', '#a0f0c8'],
  ['#ff8a4c', '#ff5c8a', '#ffd27a'],
  ['#2f5bff', '#7c5cff', '#7fe3ff'],
  ['#10b981', '#3b82f6', '#a7f3d0'],
  ['#f472b6', '#8b5cf6', '#93c5fd'],
  ['#0ea5e9', '#14b8a6', '#c4b5fd'],
]

const hash = (s) => [...s].reduce((a, c) => (a * 31 + c.charCodeAt(0)) >>> 0, 7)

// Generated cover art: a gradient scene with frosted mock windows, so each entry gets a distinct "preview" without external images.
export default function Cover({ item, kind, variant = 0, big = false, src }) {
  const h = hash(item.id)
  const [a, b, c] = PALETTES[h % PALETTES.length]
  const v = (h + variant) % 3
  const style = { '--a': a, '--b': b, '--c': c, '--ang': `${135 + variant * 38}deg`, '--shift': `${variant * 9}%` }

  if (src) {
    return (
      <div className={`cover ${big ? 'cover-big' : ''}`} style={style}>
        <img className="cover-img" src={src} alt="" loading="lazy" />
      </div>
    )
  }

  return (
    <div className={`cover ${big ? 'cover-big' : ''}`} style={style} aria-hidden>
      <span className="orb o1" />
      <span className="orb o2" />
      <span className="orb o3" />
      {kind === 'site' && <SiteMock item={item} v={v} />}
      {kind === 'repo' && <RepoMock item={item} v={v} />}
      {kind === 'doc' && <DocMock item={item} v={v} />}
    </div>
  )
}

function Chrome({ label, mono }) {
  return (
    <div className="mw-bar">
      <i /><i /><i />
      <span className={mono ? 'mw-url mono' : 'mw-url'}>{label}</span>
    </div>
  )
}

function SiteMock({ item, v }) {
  const host = new URL(item.url).hostname.replace('www.', '')
  return (
    <>
      <div className="mw mw-back">
        <Chrome label={host} />
        <div className="mw-body"><b className="l w60" /><b className="l w40" /><div className="tiles"><u /><u /><u /></div></div>
      </div>
      <div className="mw mw-front">
        <Chrome label={host} />
        <div className="mw-body">
          {v === 0 && (
            <>
              <strong className="mw-h">{item.name}</strong>
              <b className="l w80" /><b className="l w55" />
              <div className="chips"><u /><u /><u /></div>
            </>
          )}
          {v === 1 && (
            <>
              <div className="bubble me" />
              <div className="bubble ai"><b className="l w80" /><b className="l w60" /><b className="l w40" /></div>
              <div className="input" />
            </>
          )}
          {v === 2 && (
            <>
              <strong className="mw-h">{item.name}</strong>
              <div className="tiles"><u /><u /><u /><u /><u /><u /></div>
            </>
          )}
        </div>
      </div>
    </>
  )
}

function RepoMock({ item, v }) {
  const [owner, name] = item.repo.split('/')
  return (
    <>
      <div className="mw mw-back dark">
        <Chrome label={`${owner}/${name}`} mono />
        <div className="mw-body"><b className="l w70" /><b className="l w50" /><b className="l w60" /></div>
      </div>
      <div className="mw mw-front dark">
        <Chrome label="~/projects" mono />
        <div className="mw-body mono">
          {v === 0 && (
            <>
              <code><em>$</em> git clone {owner}/{name}</code>
              <code className="dim">Cloning into '{name}'…</code>
              <code><em>$</em> cd {name} && go</code>
              <div className="bars"><u /><u /><u /><u /><u /><u /><u /></div>
            </>
          )}
          {v === 1 && (
            <>
              <strong className="mw-h mono"># {name}</strong>
              <b className="l w85" /><b className="l w60" /><b className="l w75" />
              <div className="chips"><u /><u /></div>
            </>
          )}
          {v === 2 && (
            <>
              <code className="dim">// {item.lang}</code>
              <b className="l w50 k" /><b className="l w70 g" /><b className="l w35 k" /><b className="l w60 g" />
              <b className="l w45 k" />
            </>
          )}
        </div>
      </div>
    </>
  )
}

function DocMock({ item, v }) {
  const n = Math.min(item.steps.length, 4)
  return (
    <>
      <div className="mw mw-back sheet"><b className="l w60" /><b className="l w80" /><b className="l w40" /></div>
      <div className="mw mw-front sheet">
        <strong className="mw-h">{item.title.length > 14 ? item.title.slice(0, 13) + '…' : item.title}</strong>
        {Array.from({ length: n }).map((_, i) => (
          <div className="step" key={i}>
            <span>{String(i + 1).padStart(2, '0')}</span>
            <b className={`l w${[80, 60, 70, 50][(i + v) % 4]}`} />
          </div>
        ))}
      </div>
    </>
  )
}
