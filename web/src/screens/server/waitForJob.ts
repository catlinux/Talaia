import type { Job } from '../../api/containers'

/** giveUpAfter is how long a screen waits for a job before it stops. */
export const giveUpAfter = 120_000

/** JobReader is the part of an API that reads the progress of a job. */
export interface JobReader {
  job(id: string): Promise<Job>
}

/** TimedOut is thrown when a job does not finish in time. */
export class TimedOut extends Error {
  constructor() {
    super('the job did not finish in time')
    this.name = 'TimedOut'
  }
}

/**
 * waitForJob asks for the job every pollMs until it finishes and returns it.
 * onProgress receives every poll, including the last one. It stops quietly
 * (returns null) when alive() turns false, for example when the screen was
 * closed, and throws TimedOut after timeoutMs (giveUpAfter by default).
 */
export async function waitForJob(
  api: JobReader,
  jobId: string,
  pollMs: number,
  alive: () => boolean,
  timeoutMs = giveUpAfter,
  onProgress?: (job: Job) => void,
): Promise<Job | null> {
  const deadline = Date.now() + timeoutMs
  for (;;) {
    await new Promise((resolve) => setTimeout(resolve, pollMs))
    if (!alive()) {
      return null
    }
    const job = await api.job(jobId)
    onProgress?.(job)
    if (job.state === 'succeeded' || job.state === 'failed') {
      return job
    }
    if (Date.now() > deadline) {
      throw new TimedOut()
    }
  }
}
