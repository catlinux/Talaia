import { AgentUpdateError } from '../../api/servers'

/** agentUpdateErrorKey returns the translation key of a refused update. */
export function agentUpdateErrorKey(error: unknown): string {
  if (!(error instanceof AgentUpdateError)) {
    return ''
  }
  switch (error.code) {
    case 'agent_offline':
      return 'agentUpdate.errors.agent_offline'
    case 'no_update':
      return 'agentUpdate.errors.no_update'
    case 'update_in_progress':
      return 'agentUpdate.errors.update_in_progress'
    case 'session_required':
      return 'agentUpdate.errors.session_required'
    case 'not_found':
      return 'agentUpdate.errors.not_found'
    default:
      return 'agentUpdate.errors.generic'
  }
}
