import type { ContainersApi, Job } from '../../api/containers'

/** giveUpAfter is how long a screen waits for a job before it stops. */
export const giveUpAfter = 120_000

/** TimedOut is thrown when a job does not finish in time. */
export class TimedOut extends Error {
  constructor() {
    super('the job did not finish in time')
    this.name = 'TimedOut'
  }
}

/**
 * waitForJob asks for the job every pollMs until it finishes and returns it.
 * It stops quietly (returns null) when alive() turns false, for example when the
 * screen was closed, and throws TimedOut after giveUpAfter.
 */
export async function waitForJob(
  api: ContainersApi,
  jobId: string,
  pollMs: number,
  alive: () => boolean,
): Promise<Job | null> {
  const deadline = Date.now() + giveUpAfter
  for (;;) {
    await new Promise((resolve) => setTimeout(resolve, pollMs))
    if (!alive()) {
      return null
    }
    const job = await api.job(jobId)
    if (job.state === 'succeeded' || job.state === 'failed') {
      return job
    }
    if (Date.now() > deadline) {
      throw new TimedOut()
    }
  }
}
