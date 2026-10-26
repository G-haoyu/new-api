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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import type { ColumnDef } from '@tanstack/react-table'
import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  DataTableDetailRow,
  DataTableExpansionToggle,
  DataTablePage,
  useDataTable,
} from '@/components/data-table'
import {
  SideDrawerSection,
  SideDrawerSectionHeader,
  sideDrawerContentClassName,
  sideDrawerFooterClassName,
  sideDrawerHeaderClassName,
} from '@/components/drawer-layout'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { JsonEditor } from '@/components/json-editor'
import { LoadingState } from '@/components/loading-state'
import { StatusBadge } from '@/components/status-badge'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { Input } from '@/components/ui/input'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { getChannelTypeLabel } from '@/features/channels/lib/channel-utils'
import { useDebounce } from '@/hooks'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { getServerErrorMessage } from '@/lib/server-error-message'

import {
  deleteChannelModelCapability,
  getCapabilityTargets,
  getChannelModelCapability,
  saveChannelModelCapability,
} from '../api'
import { getNameRuleConfig } from '../constants'
import { capabilityTargetsQueryKeys } from '../lib'
import type { CapabilityTarget, CapabilityTargetChannel } from '../types'

const route = getRouteApi('/_authenticated/models/$section')

type Selection = {
  target: CapabilityTarget
  channel: CapabilityTargetChannel
}

const EMPTY_TARGETS: CapabilityTarget[] = []

/**
 * 一行一个模型，行内展开该模型的全部服务渠道。模型名可能由前缀/后缀/包含声明
 * 展开而来，此时在名称下面标出声明来源。
 */
export function CapabilitySettings() {
  const { t } = useTranslation()
  const [selection, setSelection] = useState<Selection | null>(null)
  const state = useTableUrlState({
    search: route.useSearch(),
    navigate: route.useNavigate(),
    pagination: {
      pageKey: 'cPage',
      pageSizeKey: 'cPageSize',
      defaultPageSize: 20,
    },
    globalFilter: { enabled: true, key: 'cFilter' },
    columnFilters: [
      { columnId: 'channel_id', searchKey: 'cChannel', type: 'string' },
    ],
  })
  const { onColumnFiltersChange } = state
  const channelFilter =
    (state.columnFilters.find((filter) => filter.id === 'channel_id')?.value as
      | string
      | undefined) ?? ''
  const channelID = Number.parseInt(channelFilter, 10) || 0
  const keyword = state.globalFilter?.trim() ?? ''

  const [channelDraft, setChannelDraft] = useState(channelFilter)
  useEffect(() => setChannelDraft(channelFilter), [channelFilter])
  const debouncedChannel = useDebounce(channelDraft, 400)
  useEffect(() => {
    const next = debouncedChannel.replaceAll(/\D/g, '')
    if (next === channelFilter) {
      return
    }
    onColumnFiltersChange((current) => {
      const rest = current.filter((filter) => filter.id !== 'channel_id')
      return next ? [...rest, { id: 'channel_id', value: next }] : rest
    })
  }, [debouncedChannel, channelFilter, onColumnFiltersChange])

  const params = {
    keyword: keyword || undefined,
    channel_id: channelID || undefined,
    p: state.pagination.pageIndex + 1,
    page_size: state.pagination.pageSize,
  }
  const targetsQuery = useQuery({
    queryKey: capabilityTargetsQueryKeys.list(params),
    queryFn: async () => {
      const response = await getCapabilityTargets(params)
      if (!response.success || !response.data) {
        throw new Error(
          response.message || t('Failed to load capability targets')
        )
      }
      return response.data
    },
  })

  const targets = targetsQuery.data?.items ?? EMPTY_TARGETS

  const columns = useMemo<ColumnDef<CapabilityTarget>[]>(() => {
    const rules = getNameRuleConfig(t)
    return [
      {
        accessorKey: 'model_name',
        header: t('Model'),
        enableSorting: false,
        enableHiding: false,
        meta: { mobileTitle: true },
        cell: ({ row }) => {
          const target = row.original
          const rule =
            target.name_rule === 0
              ? null
              : rules[target.name_rule as 0 | 1 | 2 | 3]
          return (
            <div className='flex min-w-0 items-start gap-1.5'>
              {target.channels.length > 0 ? (
                <DataTableExpansionToggle row={row} className='mt-0.5' />
              ) : (
                <span className='size-6 shrink-0' aria-hidden />
              )}
              <div className='min-w-0'>
                <div
                  className='truncate font-mono text-sm'
                  title={target.model_name}
                >
                  {target.model_name}
                </div>
                <div className='text-muted-foreground mt-0.5 truncate text-xs'>
                  {rule
                    ? `${rule.label} · ${target.declaration_name}`
                    : t('Exact model')}
                </div>
              </div>
            </div>
          )
        },
      },
      {
        id: 'channels',
        header: t('Serving channels'),
        size: 150,
        enableSorting: false,
        cell: ({ row }) => (
          <span className='tabular-nums'>{row.original.channels.length}</span>
        ),
      },
      {
        id: 'narrowed',
        header: t('Narrowed'),
        size: 140,
        enableSorting: false,
        cell: ({ row }) => {
          const total = row.original.channels.length
          const configured = row.original.channels.filter(
            (channel) => channel.configured
          ).length
          return (
            <Badge
              variant={configured > 0 ? 'default' : 'outline'}
              className='tabular-nums'
            >
              {configured} / {total}
            </Badge>
          )
        },
      },
    ]
  }, [t])

  const { table } = useDataTable({
    data: targets,
    columns,
    totalCount: targetsQuery.data?.total ?? 0,
    getRowId: (target) => target.model_name,
    columnFilters: state.columnFilters,
    pagination: state.pagination,
    globalFilter: state.globalFilter,
    onColumnFiltersChange: state.onColumnFiltersChange,
    onPaginationChange: state.onPaginationChange,
    onGlobalFilterChange: state.onGlobalFilterChange,
    manualPagination: true,
    manualFiltering: true,
    withExpandedRowModel: true,
    ensurePageInRange: state.ensurePageInRange,
  })

  const handleSelect = (target: CapabilityTarget) => {
    return (channel: CapabilityTargetChannel) =>
      setSelection({ target, channel })
  }
  const hierarchyHint = t(
    'Model-level declarations live in the model editor. Narrow them per channel here; a channel can only reduce the model-level declaration.'
  )

  let content: ReactNode
  if (targetsQuery.isError) {
    content = (
      <ErrorState
        description={targetsQuery.error.message}
        onRetry={() => void targetsQuery.refetch()}
      />
    )
  } else {
    content = (
      <DataTablePage
        table={table}
        columns={columns}
        isLoading={targetsQuery.isPending}
        isFetching={targetsQuery.isFetching}
        emptyTitle={t('No model declarations found')}
        emptyDescription={t('Try adjusting your search')}
        mobile={
          <CapabilityMobileList
            targets={targets}
            loading={targetsQuery.isPending}
            onSelect={setSelection}
          />
        }
        renderRowDetail={(row) => (
          <DataTableDetailRow row={row}>
            <div className='px-4 py-3'>
              <CapabilityChannelList
                target={row.original}
                onSelect={handleSelect(row.original)}
              />
            </div>
          </DataTableDetailRow>
        )}
        afterTable={
          <p className='text-muted-foreground shrink-0 px-1 text-xs'>
            {hierarchyHint}
          </p>
        }
        toolbarProps={{
          searchPlaceholder: t('Filter by model name...'),
          searchDebounceMs: 400,
          hideViewOptions: true,
          additionalSearch: (
            <Input
              className='w-[150px]'
              inputMode='numeric'
              placeholder={t('Channel ID')}
              aria-label={t('Filter by channel ID')}
              value={channelDraft}
              onChange={(event) =>
                setChannelDraft(event.target.value.replaceAll(/\D/g, ''))
              }
            />
          ),
          hasAdditionalFilters: channelID > 0,
          onReset: () => setChannelDraft(''),
        }}
      />
    )
  }

  return (
    <>
      {content}
      <CapabilityEditor
        selection={selection}
        onClose={() => setSelection(null)}
      />
    </>
  )
}

function CapabilityChannelList(props: {
  target: CapabilityTarget
  onSelect: (channel: CapabilityTargetChannel) => void
}) {
  const { t } = useTranslation()
  if (props.target.channels.length === 0) {
    return (
      <p className='text-muted-foreground text-sm'>
        {t('No enabled channel serves this model.')}
      </p>
    )
  }
  return (
    <ul className='grid gap-1.5 sm:grid-cols-2 xl:grid-cols-3'>
      {props.target.channels.map((channel) => (
        <li
          key={channel.id}
          className='bg-background flex min-w-0 items-center justify-between gap-2 rounded-md border px-2.5 py-1.5'
        >
          <div className='min-w-0'>
            <div className='truncate text-sm' title={channel.name}>
              #{channel.id} · {channel.name}
            </div>
            <div className='text-muted-foreground text-xs'>
              {t(getChannelTypeLabel(channel.type))}
            </div>
          </div>
          <div className='flex shrink-0 items-center gap-1.5'>
            <StatusBadge
              label={channel.configured ? t('Narrowed') : t('Inherited')}
              variant={channel.configured ? 'blue' : 'neutral'}
              size='sm'
            />
            <Button
              size='sm'
              variant='outline'
              className='h-7'
              onClick={() => props.onSelect(channel)}
            >
              {channel.configured ? t('Edit narrowing') : t('Narrow')}
            </Button>
          </div>
        </li>
      ))}
    </ul>
  )
}

function CapabilityMobileList(props: {
  targets: CapabilityTarget[]
  loading: boolean
  onSelect: (selection: Selection) => void
}) {
  const { t } = useTranslation()
  const rules = getNameRuleConfig(t)
  if (props.loading) {
    return <LoadingState />
  }
  if (props.targets.length === 0) {
    return <EmptyState title={t('No model declarations found')} />
  }
  return (
    <div className='space-y-2'>
      {props.targets.map((target) => {
        const total = target.channels.length
        const configured = target.channels.filter(
          (channel) => channel.configured
        ).length
        const rule =
          target.name_rule === 0
            ? null
            : rules[target.name_rule as 0 | 1 | 2 | 3]
        return (
          <Collapsible key={target.model_name} className='rounded-lg border'>
            <CollapsibleTrigger className='flex w-full items-center justify-between gap-2 px-3 py-2 text-left'>
              <span className='min-w-0'>
                <span className='block truncate font-mono text-sm'>
                  {target.model_name}
                </span>
                <span className='text-muted-foreground block truncate text-xs'>
                  {rule
                    ? `${rule.label} · ${target.declaration_name}`
                    : t('Exact model')}
                </span>
              </span>
              <Badge variant={configured > 0 ? 'default' : 'outline'}>
                {configured} / {total}
              </Badge>
            </CollapsibleTrigger>
            <CollapsibleContent className='border-t px-3 py-2'>
              <CapabilityChannelList
                target={target}
                onSelect={(channel) => props.onSelect({ target, channel })}
              />
            </CollapsibleContent>
          </Collapsible>
        )
      })}
    </div>
  )
}

function CapabilityEditor(props: {
  selection: Selection | null
  onClose: () => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [draft, setDraft] = useState('')
  const [serverError, setServerError] = useState('')

  const detailQuery = useQuery({
    queryKey: [
      'channel-model-capabilities',
      props.selection?.channel.id ?? 0,
      props.selection?.target.model_name ?? '',
    ],
    queryFn: async () => {
      if (!props.selection) {
        throw new Error(t('No channel selected'))
      }
      const response = await getChannelModelCapability({
        channel_id: props.selection.channel.id,
        model: props.selection.target.model_name,
      })
      if (!response.success || !response.data) {
        throw new Error(
          response.message || t('Failed to load capability detail')
        )
      }
      return response.data
    },
    enabled: Boolean(props.selection),
  })

  const detail = detailQuery.data
  useEffect(() => {
    if (!detail) return
    setDraft(detail.capabilities || '')
    setServerError('')
  }, [detail])

  const baseline = useMemo(() => {
    if (!detail?.model_capabilities) return ''
    return formatJson(detail.model_capabilities)
  }, [detail?.model_capabilities])

  const refresh = async () => {
    await queryClient.invalidateQueries({
      queryKey: capabilityTargetsQueryKeys.all,
    })
  }

  const save = useMutation({
    mutationFn: async () => {
      if (!props.selection) return
      const response = await saveChannelModelCapability({
        channel_id: props.selection.channel.id,
        model: props.selection.target.model_name,
        capabilities: draft,
      })
      if (!response.success) {
        throw new Error(response.message || t('Operation failed'))
      }
    },
    onSuccess: async () => {
      await refresh()
      await detailQuery.refetch()
      toast.success(t('Channel capability saved'))
    },
    onError: (error) => {
      setServerError(
        getServerErrorMessage(error, t('Failed to save channel capability'))
      )
    },
  })

  const reset = useMutation({
    mutationFn: async () => {
      if (!props.selection) return
      const response = await deleteChannelModelCapability({
        channel_id: props.selection.channel.id,
        model: props.selection.target.model_name,
      })
      if (!response.success) {
        throw new Error(response.message || t('Operation failed'))
      }
    },
    onSuccess: async () => {
      setDraft('')
      await refresh()
      await detailQuery.refetch()
      toast.success(t('Channel now inherits the model-level declaration'))
    },
    onError: (error) => {
      setServerError(
        getServerErrorMessage(error, t('Failed to reset channel capability'))
      )
    },
  })

  const open = Boolean(props.selection)
  let editorContent: ReactNode
  if (detailQuery.isError) {
    editorContent = (
      <div className='px-4'>
        <ErrorState
          description={detailQuery.error.message}
          onRetry={() => void detailQuery.refetch()}
        />
      </div>
    )
  } else if (detailQuery.isPending && open) {
    editorContent = (
      <div className='px-4'>
        <LoadingState />
      </div>
    )
  } else {
    editorContent = (
      <div className='space-y-6 overflow-y-auto px-4 pb-4'>
        <SideDrawerSection>
          <SideDrawerSectionHeader
            title={t('Model-level declaration (read-only)')}
            description={t(
              'The channel narrowing is merged over this declaration and can only reduce it.'
            )}
          />
          <pre className='bg-muted/40 mt-3 max-h-64 overflow-auto rounded-md border p-3 font-mono text-xs leading-relaxed'>
            {baseline || t('No model-level declaration.')}
          </pre>
        </SideDrawerSection>

        <SideDrawerSection>
          <SideDrawerSectionHeader
            title={t('Channel narrowing')}
            description={t(
              'Leave empty to inherit the model-level declaration. Server validation rejects dimensions that the model level does not declare.'
            )}
          />
          <JsonEditor
            value={draft}
            onChange={setDraft}
            defaultMode='json'
            keyLabel={t('Dimension')}
            valueLabel={t('Value')}
            emptyMessage={t('No narrowing configured.')}
          />
          {serverError ? (
            <Alert variant='destructive' className='mt-3'>
              <AlertDescription className='break-words'>
                {serverError}
              </AlertDescription>
            </Alert>
          ) : null}
          <div className='mt-3 flex flex-wrap gap-2'>
            <Button
              type='button'
              variant='outline'
              size='sm'
              disabled={!detail?.model_capabilities}
              onClick={() =>
                setDraft(
                  detail?.model_capabilities
                    ? formatJson(detail.model_capabilities)
                    : ''
                )
              }
            >
              {t('Copy model-level declaration')}
            </Button>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => setDraft('')}
            >
              {t('Clear')}
            </Button>
          </div>
        </SideDrawerSection>
      </div>
    )
  }

  return (
    <Sheet
      open={open}
      onOpenChange={(value) => {
        if (!value) props.onClose()
      }}
    >
      <SheetContent className={sideDrawerContentClassName('sm:max-w-[720px]')}>
        <SheetHeader className={sideDrawerHeaderClassName()}>
          <SheetTitle>{t('Channel capability narrowing')}</SheetTitle>
          <SheetDescription>
            {props.selection
              ? `#${props.selection.channel.id} · ${props.selection.channel.name} · ${props.selection.target.model_name}`
              : ''}
          </SheetDescription>
        </SheetHeader>

        {editorContent}

        <SheetFooter className={sideDrawerFooterClassName()}>
          <Button
            variant='outline'
            onClick={() => props.onClose()}
            disabled={save.isPending || reset.isPending}
          >
            {t('Cancel')}
          </Button>
          <Button
            variant='outline'
            onClick={() => reset.mutate()}
            disabled={!detail?.configured || save.isPending || reset.isPending}
          >
            {t('Reset to inherit')}
          </Button>
          <Button
            onClick={() => save.mutate()}
            disabled={!open || save.isPending || reset.isPending}
          >
            {t('Save')}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}

function formatJson(value: string): string {
  try {
    return JSON.stringify(JSON.parse(value), null, 2)
  } catch {
    return value
  }
}
