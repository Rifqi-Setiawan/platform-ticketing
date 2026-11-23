import type { Section } from './api'

export function Stadium({
  sections,
  selected,
  locked,
  onPick,
}: {
  sections: Section[]
  selected?: string
  locked: boolean
  onPick: (section: Section) => void
}) {
  const stands = sections
    .slice()
    .sort((a, b) => Number(a.name === 'VIP') - Number(b.name === 'VIP'))
  function stand(section: Section | undefined, position: string) {
    return (
      <button
        type="button"
        className={
          'map-stand ' + position + (section?.id === selected ? ' current' : '')
        }
        disabled={locked || !section || section.available === 0}
        onClick={() => section && onPick(section)}
        aria-pressed={section?.id === selected}
        aria-label={
          section ? 'Select ' + section.name + ' stand' : 'Loading stand'
        }
      >
        <span>{section?.name || '—'}</span>
      </button>
    )
  }
  return (
    <div className="stadium" aria-label="Stand selection map">
      <div className="stadium-bowl">
        {stand(stands[0], 'north')}
        <div className="seats seats-left" />
        <div className="seats seats-right" />
        <div className="grass">
          <div className="pitch">
            <i className="circle" />
            <i className="halfway" />
            <i className="penalty l" />
            <i className="penalty r" />
            <i className="goal l" />
            <i className="goal r" />
          </div>
        </div>
        {stand(stands[1], 'south')}
      </div>
      <span className="map-caption">
        <i /> Stand map
      </span>
    </div>
  )
}
