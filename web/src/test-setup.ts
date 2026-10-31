import { configure } from '@testing-library/react'

// The default of one second is too short when the whole suite runs in parallel
// on a busy machine; a test that waits for an element fails only on timing.
configure({ asyncUtilTimeout: 20_000 })
