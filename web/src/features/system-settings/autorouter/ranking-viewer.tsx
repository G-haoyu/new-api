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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  StaticDataTable,
  type StaticDataTableColumn,
} from '@/components/data-table'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'

import { useAutoRouterRanking } from './hooks'
import type { AutoRouterRankedModel } from './types'

/** The ranking the router routes by, one profile at a time; fetched only while open. */
export function RankingViewer() {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [selected, setSelected] = useState('')
  const ranking = useAutoRouterRanking(open)

  const profiles = ranking.data?.data.profiles ?? []
  const profile =
    profiles.find((item) => item.profile === selected) ?? profiles[0]

  const columns: StaticDataTableColumn<AutoRouterRankedModel>[] = [
    {
      id: 'rank',
      header: t('Rank'),
      className: 'w-16',
      cellClassName: 'text-muted-foreground tabular-nums',
      cell: (_row, index) => index + 1,
    },
    {
      id: 'model',
      header: t('Model'),
      cellClassName: 'font-mono text-xs',
      cell: (row) => row.model,
    },
    {
      id: 'score',
      header: t('Ranking score'),
      className: 'w-28 text-right',
      cellClassName: 'text-right font-mono tabular-nums',
      cell: (row) => (row.score === null ? '-' : row.score.toFixed(4)),
    },
  ]

  return (
    <div className='space-y-3'>
      <Button
        type='button'
        variant='outline'
        size='sm'
        aria-expanded={open}
        onClick={() => setOpen((current) => !current)}
      >
        {open ? t('Hide ranking') : t('Show ranking')}
      </Button>

      {open && ranking.isPending ? (
        <Skeleton className='h-40 w-full' />
      ) : null}
      {open && ranking.isError ? (
        <p className='text-destructive text-sm'>
          {t('Failed to load the ranking')}
        </p>
      ) : null}

      {open && profile ? (
        <div className='space-y-3'>
          <div className='max-w-sm space-y-1.5'>
            <Label htmlFor='autorouter-ranking-profile'>{t('Profile')}</Label>
            <Select
              items={profiles.map((item) => ({
                value: item.profile,
                label: `${item.profile} (${item.models.length})`,
              }))}
              value={profile.profile}
              onValueChange={(value) => setSelected(value ?? '')}
            >
              <SelectTrigger id='autorouter-ranking-profile' className='w-full'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {profiles.map((item) => (
                  <SelectItem key={item.profile} value={item.profile}>
                    {item.profile} ({item.models.length})
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className='max-h-96 overflow-auto rounded-md'>
            <StaticDataTable
              columns={columns}
              data={profile.models}
              getRowKey={(row) => row.model}
              emptyContent={t('No data')}
            />
          </div>
        </div>
      ) : null}
    </div>
  )
}
