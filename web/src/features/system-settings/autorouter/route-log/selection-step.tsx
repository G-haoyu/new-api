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
import { ListOrdered } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import {
  StaticDataTable,
  type StaticDataTableColumn,
} from '@/components/data-table'
import { formatBillingCurrencyFromUSD } from '@/lib/currency'
import { cn } from '@/lib/utils'

import { formatScore, ladderOutcomeLabel, skipReasonLabel } from './labels'
import { StepFrame, ToneBadge } from './route-process-parts'
import type { RoutePick, RoutePickCandidate, RouteTraceStep } from './types'

/** The candidates the quality and price comparison weighed, the chosen one marked. */
export function PickTable(props: { pick: RoutePick }) {
  const { t } = useTranslation()
  const columns: StaticDataTableColumn<RoutePickCandidate>[] = [
    {
      id: 'model',
      header: t('Model'),
      cell: (row) => (
        <div className='flex flex-wrap items-center gap-1.5'>
          <span className='font-mono'>{row.model}</span>
          {row.model === props.pick.chosen && (
            <ToneBadge tone='success' label={t('Chosen')} />
          )}
        </div>
      ),
    },
    {
      id: 'score',
      header: t('Leaderboard score'),
      cellClassName: 'tabular-nums',
      cell: (row) => formatScore(row.score),
    },
    {
      id: 'price',
      header: t('Price per million tokens'),
      cellClassName: 'tabular-nums',
      cell: (row) =>
        formatBillingCurrencyFromUSD(row.price, {
          digitsLarge: 4,
          digitsSmall: 6,
          abbreviate: false,
        }),
    },
    {
      id: 'discount',
      header: t('Price discount'),
      cellClassName: 'tabular-nums',
      cell: (row) => formatScore(row.discount),
    },
    {
      id: 'utility',
      header: t('Score after discount'),
      cellClassName: 'font-medium tabular-nums',
      cell: (row) => formatScore(row.utility),
    },
  ]

  return (
    <div className='space-y-1'>
      <p className='text-muted-foreground text-xs'>
        {t(
          'Candidates within {{weight}} of the best score, ranked by score after the price discount.',
          { weight: formatScore(props.pick.weight) }
        )}
      </p>
      <StaticDataTable
        columns={columns}
        data={props.pick.band}
        getRowKey={(row) => row.model}
        getRowClassName={(row) =>
          cn(row.model === props.pick.chosen && 'bg-success/10 font-semibold')
        }
      />
    </div>
  )
}

function StepSummary(props: { step: RouteTraceStep }) {
  const { t } = useTranslation()
  const step = props.step
  const profile = step.profile || t('Default models')

  return (
    <div className='space-y-1'>
      <div className='flex flex-wrap items-baseline gap-x-2 text-xs'>
        <span className='font-mono font-medium'>{profile}</span>
        {step.models ? (
          <span className='text-muted-foreground'>
            {t('{{count}} models ranked', { count: step.models })}
          </span>
        ) : null}
        <span className='text-muted-foreground'>
          {ladderOutcomeLabel(t, step.outcome)}
        </span>
        {step.model && (
          <span className='text-success font-mono font-medium'>
            → {step.model}
          </span>
        )}
      </div>
      {step.skipped && step.skipped.length > 0 && (
        <ul className='text-muted-foreground space-y-0.5 ps-3 text-xs'>
          {step.skipped.map((skipped) => (
            <li key={skipped.model}>
              <span className='font-mono'>{skipped.model}</span>{' '}
              ({formatScore(skipped.score)}):{' '}
              {skipReasonLabel(t, skipped.reason)}
            </li>
          ))}
          {step.skipped_more ? (
            <li>{t('and {{count}} more', { count: step.skipped_more })}</li>
          ) : null}
        </ul>
      )}
    </div>
  )
}

/** Every ranking tried in order, and why a better-ranked model was passed over. */
export function SelectionStep(props: {
  ladder: RouteTraceStep[]
  pick?: RoutePick
}) {
  const { t } = useTranslation()
  // A ranking that compares successfully ends the walk, so at most one step did.
  const compared = props.ladder.find(
    (step) => step.outcome === 'compared' && step.model === props.pick?.chosen
  )
  // A ranking can be tried twice (its class, then the defaults), so repeats are numbered.
  const seen = new Map<string, number>()
  const rows = props.ladder.map((step) => {
    const name = step.profile ?? 'default'
    const occurrence = (seen.get(name) ?? 0) + 1
    seen.set(name, occurrence)
    return { step, key: `${name}#${occurrence}` }
  })

  return (
    <StepFrame icon={ListOrdered} title={t('Model selection')}>
      {rows.map((row) => (
        <div key={row.key} className='space-y-2'>
          <StepSummary step={row.step} />
          {row.step === compared && props.pick && (
            <PickTable pick={props.pick} />
          )}
        </div>
      ))}
    </StepFrame>
  )
}
