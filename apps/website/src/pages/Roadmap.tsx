import { roadmap } from 'virtual:roadmap'
import type { RoadmapIssue, RoadmapStage } from '../content/types'
import { Eyebrow } from '../components/Brand'
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
    <section className="roadmap-stage" aria-labelledby={titleId}>
      <div className="roadmap-stage__head">
        <Eyebrow>{copy.stage(index)}</Eyebrow>
        <h2 id={titleId}>{stage.title}</h2>
        <p className="roadmap-stage__count">{copy.closed(stage.closed, total)}</p>
        <div className="progress" aria-hidden="true">
          <span style={{ width: `${total === 0 ? 0 : (stage.closed / total) * 100}%` }} />
        </div>
      </div>
      <div className="roadmap-groups">
        {stage.groups.map((group) => {
          // Areas are the words issue titles start with, kept as they are, except the one for titles with none.
          const area = group.area === 'Other' ? copy.other : group.area
          return (
            <section key={group.area} className="roadmap-group" aria-label={area}>
              <h3>{area}</h3>
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
      <span className="issue__state" role="img" aria-label={issue.state === 'closed' ? copy.closedState : copy.open} />
      <a href={issue.url} lang="en">
        <span className="issue__ref">{ref}</span> {issue.title}
      </a>
    </li>
  )
}

function formatDate(iso: string, locale: string): string {
  return new Date(iso).toLocaleDateString(locale, { year: 'numeric', month: 'short', day: 'numeric', timeZone: 'UTC' })
}
