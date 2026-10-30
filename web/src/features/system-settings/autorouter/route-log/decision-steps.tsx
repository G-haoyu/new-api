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
import { GitCompareArrows, ShieldCheck } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { formatScore, trustReasonLabel, type RouteTone } from './labels'
import { Chips, Facts, StepFrame, ToneBadge } from './route-process-parts'
import type { RouteTraceContinuation, RouteTraceTrust } from './types'

export function TrustStep(props: { trust: RouteTraceTrust }) {
  const { t } = useTranslation()
  const trust = props.trust
  const classes = Object.entries(trust.classes ?? {})
    .sort(([, a], [, b]) => b - a)
    .map(([name, probability]) => `${name} ${formatScore(probability)}`)
  const trusted = !trust.reason

  return (
    <StepFrame icon={ShieldCheck} title={t('Trust check')}>
      <ToneBadge
        tone={trusted ? 'success' : 'warning'}
        label={trusted ? t('Trusted') : trustReasonLabel(t, trust.reason ?? '')}
      />
      <Facts
        rows={[[t('Confidence threshold'), formatScore(trust.threshold)]]}
      />
      {classes.length > 0 && (
        <>
          <p className='text-muted-foreground text-xs'>
            {t('Probability of each task class')}
          </p>
          <Chips items={classes} />
        </>
      )}
      {trusted && trust.profiles && (
        <>
          <p className='text-muted-foreground text-xs'>
            {t('Rankings to try, most specific first')}
          </p>
          <Chips items={trust.profiles} />
        </>
      )}
      {!trusted && (
        <p className='text-muted-foreground text-xs'>
          {t(
            'An untrusted answer keeps the previous route, or uses the default models.'
          )}
        </p>
      )}
    </StepFrame>
  )
}

function continuationVerdict(
  t: (key: string) => string,
  continuation: RouteTraceContinuation
): { tone: RouteTone; label: string } {
  if (continuation.continued) {
    return { tone: 'success', label: t('Continued the previous task') }
  }
  if (continuation.task_change >= continuation.threshold) {
    return { tone: 'info', label: t('A new task') }
  }
  return { tone: 'warning', label: t('The previous model can no longer serve') }
}

export function ContinuationStep(props: {
  continuation: RouteTraceContinuation
}) {
  const { t } = useTranslation()
  const continuation = props.continuation
  const verdict = continuationVerdict(t, continuation)

  return (
    <StepFrame icon={GitCompareArrows} title={t('Continuation check')}>
      <ToneBadge tone={verdict.tone} label={verdict.label} />
      <Facts
        rows={[
          [
            t('Previous model'),
            <span key='previous' className='font-mono'>
              {continuation.previous}
            </span>,
          ],
          [
            t('Task change'),
            t('{{value}} against a threshold of {{threshold}}', {
              value: formatScore(continuation.task_change),
              threshold: formatScore(continuation.threshold),
            }),
          ],
        ]}
      />
    </StepFrame>
  )
}
