import { describe, expect, it } from 'vitest'

import {
  ROUTING_PREFERENCE_FORM_DEFAULTS,
  formValuesToRoutingPreference,
  isRoutingPreferenceChanged,
  routingPreferenceToFormValues,
} from '../routing-preference'

describe('token routing preference form mapping', () => {
  it('treats a missing preference as the disabled platform default', () => {
    expect(routingPreferenceToFormValues(null)).toEqual(
      ROUTING_PREFERENCE_FORM_DEFAULTS
    )
    expect(
      formValuesToRoutingPreference(ROUTING_PREFERENCE_FORM_DEFAULTS)
    ).toBeNull()
  })

  it('round-trips the sort mode only', () => {
    const values = routingPreferenceToFormValues({ mode: 'price' })
    expect(values).toEqual({ enabled: true, mode: 'price' })
    expect(formValuesToRoutingPreference(values)).toEqual({ mode: 'price' })
  })

  it('falls back to balanced for an unknown stored mode', () => {
    expect(
      routingPreferenceToFormValues({ mode: 'legacy' as never }).mode
    ).toBe('balanced')
  })

  it('detects changes against the stored preference', () => {
    const stored = { mode: 'price' as const }
    const values = routingPreferenceToFormValues(stored)
    expect(isRoutingPreferenceChanged(values, stored)).toBe(false)
    expect(
      isRoutingPreferenceChanged({ ...values, mode: 'latency' }, stored)
    ).toBe(true)
    expect(
      isRoutingPreferenceChanged({ ...values, enabled: false }, stored)
    ).toBe(true)
    expect(
      isRoutingPreferenceChanged(ROUTING_PREFERENCE_FORM_DEFAULTS, null)
    ).toBe(false)
  })
})
