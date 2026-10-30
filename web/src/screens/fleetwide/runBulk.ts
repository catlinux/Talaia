import type { JobReader } from '../server/waitForJob'
import { waitForJob } from '../server/waitForJob'

export interface BulkJob {
  serverId: string
  /** run sends the action and returns the id of its job. */
  run: () => Promise<string>
}

export interface BulkOutcome {
  done: number
  failed: number
}

/**
 * runBulkJobs runs at most two servers at once and the jobs of one server one
 * after another, so a fan-out cannot flood the agents.
 */
export async function runBulkJobs(
  jobs: BulkJob[],
  api: JobReader,
  pollMs: number,
  alive: () => boolean,
): Promise<BulkOutcome> {
  const groups = new Map<string, BulkJob[]>()
  for (const job of jobs) {
    const group = groups.get(job.serverId)
    if (group === undefined) groups.set(job.serverId, [job])
    else group.push(job)
  }

  const queues = [...groups.values()]
  let next = 0
  const outcome: BulkOutcome = { done: 0, failed: 0 }

  const worker = async () => {
    for (;;) {
      const index = next
      next += 1
      const queue = queues[index]
      if (queue === undefined) return
      for (const job of queue) {
        try {
          const jobId = await job.run()
          const result = await waitForJob(api, jobId, pollMs, alive)
          if (result === null) return
          if (result.state === 'succeeded') outcome.done += 1
          else outcome.failed += 1
        } catch {
          outcome.failed += 1
        }
        if (!alive()) return
      }
    }
  }

  await Promise.all(
    Array.from({ length: Math.min(2, queues.length) }, () => {
      return worker()
    }),
  )
  return outcome
}
