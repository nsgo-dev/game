import { roadmap } from 'virtual:roadmap'
import type { RoadmapIssue, RoadmapStage } from '../content/types'
import { Eyebrow } from '../components/Brand'
import { Icon } from '../components/Icon'
import { Segments } from '../components/Segments'
import { type Copy, copies } from '../copy'
import { type Lang, pageHead, useLang } from '../i18n'
import { GITHUB_URL, PLAN_URL } from '../links'

export const roadmapHead = (lang: Lang) => pageHead(lang, 'roadmap/', copies[lang].roadmap.title, copies[lang].roadmap.description)

// The issues' titles are GitHub's, in English in either language; the page's own words follow its language.
export function Roadmap() {
  const copy = copies[useLang()].roadmap
  return (
    <div className="container roadmap">
      <header className="page-head">
        <h1>{copy.heading}</h1>
        <p>
          {copy.intro[0]}
          <a href={`${GITHUB_URL}/issues`}>nsgo-dev/game</a>
          {copy.intro[1]}
          <a href={PLAN_URL}>nsgo#1047</a>
          {copy.intro[2]}
          {roadmap.status === 'ok' && (
            <>
              {copy.asOf[0]}
              <time dateTime={roadmap.fetchedAt}>{formatDate(roadmap.fetchedAt, copy.dateLocale)}</time>
              {copy.asOf[1]}
            </>
          )}
        </p>
      </header>
      {roadmap.status === 'ok' ? (
        roadmap.stages.map((stage, i) => <Stage key={stage.id} stage={stage} index={i + 1} copy={copy} />)
      ) : (
        <p className="notice" role="status">
          {copy.unavailable[0]}
          {roadmap.reason}
          {copy.unavailable[1]}
          <a href={PLAN_URL}>nsgo#1047</a>
          {copy.unavailable[2]}
        </p>
      )}
    </div>
  )
}

function Stage({ stage, index, copy }: { stage: RoadmapStage; index: number; copy: Copy['roadmap'] }) {
  const total = stage.open + stage.closed
  const titleId = `stage-${stage.id}`
  return (
    <section className="roadmap-stage hud" aria-labelledby={titleId}>
      <div className="roadmap-stage__head">
        <Eyebrow>{copy.stage(index)}</Eyebrow>
        <h2 id={titleId}>{stage.title}</h2>
        <p className="roadmap-stage__count">
          <span className="roadmap-stage__xp" aria-hidden="true">
            {copy.xp} {stage.closed}/{total}
          </span>
          <span>{copy.closed(stage.closed, total)}</span>
        </p>
        <Segments closed={stage.closed} total={total} />
      </div>
      <div className="roadmap-groups">
        {stage.groups.map((group) => {
          // Areas are the words issue titles start with, kept as they are, except the one for titles with none.
          const area = group.area === 'Other' ? copy.other : group.area
          const done = group.issues.filter((issue) => issue.state === 'closed').length
          return (
            <section key={group.area} className="roadmap-group" aria-label={area}>
              <h3>
                <span className="hud-label">{copy.quest}</span>
                <span className="roadmap-group__name">{area}</span>
                <span className="roadmap-group__count">
                  <span className="sr-only">{copy.closed(done, group.issues.length)}</span>
                  <span aria-hidden="true">
                    {done}/{group.issues.length}
                  </span>
                </span>
              </h3>
              <ul>
                {group.issues.map((issue) => (
                  <Issue key={`${issue.repo}#${issue.number}`} issue={issue} copy={copy} />
                ))}
              </ul>
            </section>
          )
        })}
      </div>
    </section>
  )
}

function Issue({ issue, copy }: { issue: RoadmapIssue; copy: Copy['roadmap'] }) {
  const ref = issue.repo === 'nsgo-dev/game' ? `#${issue.number}` : `nsgo#${issue.number}`
  return (
    <li className={`issue issue--${issue.state}`}>
      <span className="issue__state" role="img" aria-label={issue.state === 'closed' ? copy.closedState : copy.open}>
        {issue.state === 'closed' && <Icon name="check" size={9} />}
      </span>
      <a href={issue.url} lang="en">
        <span className="issue__ref">{ref}</span> {issue.title}
      </a>
    </li>
  )
}

function formatDate(iso: string, locale: string): string {
  return new Date(iso).toLocaleDateString(locale, { year: 'numeric', month: 'short', day: 'numeric', timeZone: 'UTC' })
}
