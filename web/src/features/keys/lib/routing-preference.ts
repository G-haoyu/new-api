/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { z } from 'zod'

import type { RoutingMode, RoutingPreference } from '../types'

// ============================================================================
// Token-level Scheduler routing preference (one per API key, model-agnostic)
// ============================================================================

export const ROUTING_MODES = [
  'balanced',
  'price',
  'throughput',
  'latency',
  'quality',
] as const satisfies readonly RoutingMode[]

export const ROUTING_MODE_LABELS: Record<RoutingMode, string> = {
  balanced: 'Default (balanced)',
  price: 'Price (cheapest first)',
  latency: 'Latency (lowest first)',
  throughput: 'Throughput (highest first)',
  quality: 'Exacto (tool-call quality first)',
}

export const routingPreferenceFormSchema = z.object({
  enabled: z.boolean(),
  mode: z.enum(ROUTING_MODES),
})

export type RoutingPreferenceFormValues = z.infer<
  typeof routingPreferenceFormSchema
>

export const ROUTING_PREFERENCE_FORM_DEFAULTS: RoutingPreferenceFormValues = {
  enabled: false,
  mode: 'balanced',
}

function isRoutingMode(value: unknown): value is RoutingMode {
  return ROUTING_MODES.includes(value as RoutingMode)
}

/** A stored preference turns the section on; `null` means platform default. */
export function routingPreferenceToFormValues(
  preference: RoutingPreference | null | undefined
): RoutingPreferenceFormValues {
  if (!preference) return { ...ROUTING_PREFERENCE_FORM_DEFAULTS }
  return {
    enabled: true,
    mode: isRoutingMode(preference.mode)
      ? preference.mode
      : ROUTING_PREFERENCE_FORM_DEFAULTS.mode,
  }
}

/** Whether the form differs from the stored preference. */
export function isRoutingPreferenceChanged(
  values: RoutingPreferenceFormValues,
  stored: RoutingPreference | null | undefined
): boolean {
  const baseline = routingPreferenceToFormValues(stored)
  return values.enabled !== baseline.enabled || values.mode !== baseline.mode
}

/**
 * Builds the PUT payload. A disabled section clears the preference so the key
 * falls back to the platform default policy.
 */
export function formValuesToRoutingPreference(
  values: RoutingPreferenceFormValues
): RoutingPreference | null {
  return values.enabled ? { mode: values.mode } : null
}
