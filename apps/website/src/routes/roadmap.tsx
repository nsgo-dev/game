import { createFileRoute } from '@tanstack/react-router'
import { roadmap } from 'virtual:roadmap'
import type { RoadmapIssue, RoadmapStage } from '../content/types'
import { GITHUB_URL, PLAN_URL } from '../links'

export const Route = createFileRoute('/roadmap')({
  head: () => ({
    meta: [{ title: 'Roadmap · game' }, { name: 'description', content: 'Open and closed issues of nsgo-dev/game and its plan in NSGo, by stage and area.' }],
  }),
  component: Roadmap,
})

function Roadmap() {
  return (
    <div className="roadmap">
      <header className="page-head">
        <h1>Roadmap</h1>
        <p>
          The issues of <a href={`${GITHUB_URL}/issues`}>nsgo-dev/game</a> and the tasks of its plan, <a href={PLAN_URL}>nsgo#1047</a>, by stage and area.
          {roadmap.status === 'ok' && (
            <>
              {' '}
              As of <time dateTime={roadmap.fetchedAt}>{formatDate(roadmap.fetchedAt)}</time>.
            </>
          )}
        </p>
      </header>
      {roadmap.status === 'ok' ? (
        roadmap.stages.map((stage, i) => <Stage key={stage.id} stage={stage} index={i + 1} />)
      ) : (
        <p className="notice" role="status">
          This build could not read the issues from GitHub ({roadmap.reason}). See them on <a href={PLAN_URL}>nsgo#1047</a>.
        </p>
      )}
    </div>
  )
}

function Stage({ stage, index }: { stage: RoadmapStage; index: number }) {
  const total = stage.open + stage.closed
  const titleId = `stage-${stage.id}`
  return (
    <section className={`roadmap-stage stage-${stage.id}`} aria-labelledby={titleId}>
      <div className="stage-head">
        <h2 id={titleId}>
          <span className="stage-tag">Stage {index}</span> {stage.title}
        </h2>
        <p className="progress-label">
          {stage.closed} of {total} closed
        </p>
        <progress max={total} value={stage.closed} aria-hidden="true" />
      </div>
      <div className="groups">
        {stage.groups.map((group) => (
          <section key={group.area} className="group" aria-label={group.area}>
            <h3>{group.area}</h3>
            <ul>
              {group.issues.map((issue) => (
                <Issue key={`${issue.repo}#${issue.number}`} issue={issue} />
              ))}
            </ul>
          </section>
        ))}
      </div>
    </section>
  )
}

function Issue({ issue }: { issue: RoadmapIssue }) {
  const ref = issue.repo === 'nsgo-dev/game' ? `#${issue.number}` : `nsgo#${issue.number}`
  return (
    <li className={`issue ${issue.state}`}>
      <span className="state" role="img" aria-label={issue.state === 'closed' ? 'Closed' : 'Open'} />
      <a href={issue.url}>
        <span className="ref">{ref}</span> {issue.title}
      </a>
    </li>
  )
}

function formatDate(iso: string): string {
  return new Date(iso).toLocaleDateString('en', { year: 'numeric', month: 'short', day: 'numeric', timeZone: 'UTC' })
}
