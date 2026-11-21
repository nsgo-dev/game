import { roadmap } from 'virtual:roadmap'
import type { RoadmapGroup, RoadmapIssue, RoadmapItem, RoadmapStage } from '../content/types'
import { Eyebrow } from '../components/Brand'
import { Icon } from '../components/Icon'
import { Segments } from '../components/Segments'
import { type Copy, copies } from '../copy'
import { type Lang, pageHead, useLang } from '../i18n'
import { GITHUB_URL, PLAN_URL } from '../links'

export const roadmapHead = (lang: Lang) => pageHead(lang, 'roadmap/', copies[lang].roadmap.title, copies[lang].roadmap.description)

// The issues' titles are GitHub's, in English in either language; the page's own words follow its language, the stages' names and descriptions among them, and a stage the copy has no words for shows its issue's English title and description.
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
        <>
          {roadmap.stages.map((stage) => (
            <Stage key={stage.number} stage={stage} copy={copy} />
          ))}
          {roadmap.unplanned.length > 0 && <Unplanned issues={roadmap.unplanned} copy={copy} />}
        </>
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

function Stage({ stage, copy }: { stage: RoadmapStage; copy: Copy['roadmap'] }) {
  const total = stage.open + stage.closed
  const titleId = `stage-${stage.number}`
  const name = copy.stageNames[stage.number - 1]
  const translated = copy.stageDescriptions[stage.number - 1]
  const description = translated ?? stage.description
  return (
    <section className="roadmap-stage hud" aria-labelledby={titleId}>
      <div className="roadmap-stage__head">
        <Eyebrow>{copy.stage(stage.number)}</Eyebrow>
        <h2 id={titleId}>
          <a href={stage.url} lang={name ? undefined : 'en'}>
            {name ?? stage.title}
          </a>
        </h2>
        <p className="roadmap-stage__count">
          <span className="roadmap-stage__xp" aria-hidden="true">
            {copy.xp} {stage.closed}/{total}
          </span>
          <span>{copy.closed(stage.closed, total)}</span>
        </p>
        {description && (
          <p className="roadmap-stage__desc" lang={translated ? undefined : 'en'}>
            {description}
          </p>
        )}
        <Segments closed={stage.closed} total={total} />
      </div>
      <div className="roadmap-groups">
        {stage.groups.map((group) => (
          <Group key={group.epic ? issueKey(group.epic) : 'single'} group={group} copy={copy} />
        ))}
      </div>
    </section>
  )
}

function Group({ group, copy }: { group: RoadmapGroup; copy: Copy['roadmap'] }) {
  const total = group.open + group.closed
  const area = group.epic ? group.area : copy.single
  return (
    <section className="roadmap-group" aria-label={area}>
      <h3>
        <span className="hud-label">{copy.quest}</span>
        {group.epic ? (
          <a className="roadmap-group__name" href={group.epic.url} title={group.epic.title} lang="en">
            {area} <span className="issue__ref">{issueRef(group.epic)}</span>
          </a>
        ) : (
          <span className="roadmap-group__name">{area}</span>
        )}
        <span className="roadmap-group__count">
          <span className="sr-only">{copy.closed(group.closed, total)}</span>
          <span aria-hidden="true">
            {group.closed}/{total}
          </span>
        </span>
      </h3>
      <ul>
        {group.items.map((item) => (
          <Issue key={issueKey(item)} issue={item} copy={copy} />
        ))}
      </ul>
    </section>
  )
}

// nsgo-dev/game's issues in no stage of the plan, so none drop out of the page unseen.
function Unplanned({ issues, copy }: { issues: RoadmapIssue[]; copy: Copy['roadmap'] }) {
  return (
    <section className="roadmap-stage hud" aria-labelledby="stage-unplanned">
      <div className="roadmap-stage__head">
        <h2 id="stage-unplanned">{copy.unplanned}</h2>
        <p className="roadmap-stage__desc">{copy.unplannedCount(issues.length)}</p>
      </div>
      <div className="roadmap-group">
        <ul>
          {issues.map((issue) => (
            <Issue key={issueKey(issue)} issue={issue} copy={copy} />
          ))}
        </ul>
      </div>
    </section>
  )
}

function Issue({ issue, copy }: { issue: RoadmapIssue | RoadmapItem; copy: Copy['roadmap'] }) {
  const nested = 'depth' in issue && issue.depth === 1
  return (
    <li className={`issue issue--${issue.state}${nested ? ' issue--nested' : ''}`}>
      <span className="issue__state" role="img" aria-label={issue.state === 'closed' ? copy.closedState : copy.open}>
        {issue.state === 'closed' && <Icon name="check" size={9} />}
      </span>
      <a href={issue.url} lang="en">
        <span className="issue__ref">{issueRef(issue)}</span> {issue.title}
      </a>
    </li>
  )
}

function issueRef(issue: RoadmapIssue): string {
  return issue.repo === 'nsgo-dev/game' ? `#${issue.number}` : `nsgo#${issue.number}`
}

function issueKey(issue: RoadmapIssue): string {
  return `${issue.repo}#${issue.number}`
}

function formatDate(iso: string, locale: string): string {
  return new Date(iso).toLocaleDateString(locale, { year: 'numeric', month: 'short', day: 'numeric', timeZone: 'UTC' })
}
