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
import { type ReactNode, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  SideDrawerSection,
  SideDrawerSectionHeader,
  sideDrawerContentClassName,
  sideDrawerFooterClassName,
  sideDrawerHeaderClassName,
} from '@/components/drawer-layout'
import { ErrorState } from '@/components/error-state'
import { JsonEditor } from '@/components/json-editor'
import { LoadingState } from '@/components/loading-state'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { getServerErrorMessage } from '@/lib/server-error-message'

import {
  deleteChannelModelCapability,
  getCapabilityTargets,
  getChannelModelCapability,
  saveChannelModelCapability,
} from '../api'
import { capabilityTargetsQueryKeys } from '../lib'
import type { CapabilityTarget, CapabilityTargetChannel } from '../types'

type Selection = {
  target: CapabilityTarget
  channel: CapabilityTargetChannel
}

export function CapabilitySettings() {
  const { t } = useTranslation()
  const [selection, setSelection] = useState<Selection | null>(null)
  const targetsQuery = useQuery({
    queryKey: capabilityTargetsQueryKeys.list(),
    queryFn: async () => {
      const response = await getCapabilityTargets()
      if (!response.success || !response.data) {
        throw new Error(
          response.message || t('Failed to load capability targets')
        )
      }
      return response.data
    },
  })

  const targets = targetsQuery.data ?? []

  let content: ReactNode
  if (targetsQuery.isError) {
    content = (
      <ErrorState
        description={targetsQuery.error.message}
        onRetry={() => void targetsQuery.refetch()}
      />
    )
  } else if (targetsQuery.isPending) {
    content = <LoadingState />
  } else {
    content = (
      <div className='overflow-x-auto rounded-lg border'>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('Model')}</TableHead>
              <TableHead>{t('Serving channels')}</TableHead>
              <TableHead>{t('Narrowed')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {targets.length === 0 ? (
              <TableRow>
                <TableCell
                  colSpan={3}
                  className='text-muted-foreground py-8 text-center text-sm'
                >
                  {t('No exact model declarations found.')}
                </TableCell>
              </TableRow>
            ) : (
              targets.map((target) => {
                const configured = target.channels.filter(
                  (channel) => channel.configured
                ).length
                const hasBaseline = Boolean(target.capabilities?.trim())

                let channelCell: ReactNode
                if (target.channels.length === 0) {
                  channelCell = (
                    <span className='text-muted-foreground text-sm'>
                      {t('No enabled channel serves this model.')}
                    </span>
                  )
                } else if (!hasBaseline) {
                  channelCell = (
                    <span className='text-muted-foreground text-sm'>
                      {t('Configure model-level capabilities first.')}
                    </span>
                  )
                } else {
                  channelCell = (
                    <ChannelSelector
                      key={`${target.id}-${configured}`}
                      target={target}
                      onSelect={(channel) => setSelection({ target, channel })}
                    />
                  )
                }

                return (
                  <TableRow key={target.id}>
                    <TableCell className='font-medium'>
                      {target.model_name}
                    </TableCell>
                    <TableCell>{channelCell}</TableCell>
                    <TableCell>
                      <Badge variant={configured > 0 ? 'default' : 'outline'}>
                        {configured} / {target.channels.length}
                      </Badge>
                    </TableCell>
                  </TableRow>
                )
              })
            )}
          </TableBody>
        </Table>
      </div>
    )
  }

  return (
    <div className='space-y-4'>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Model-level declarations live in the model editor. Narrow them per channel here; a channel can only reduce the model-level declaration.'
        )}
      </p>
      {content}
      <CapabilityEditor
        selection={selection}
        onClose={() => setSelection(null)}
      />
    </div>
  )
}

function ChannelSelector(props: {
  target: CapabilityTarget
  onSelect: (channel: CapabilityTargetChannel) => void
}) {
  const { t } = useTranslation()
  return (
    <Select
      value=''
      onValueChange={(value) => {
        const channel = props.target.channels.find(
          (item) => String(item.id) === value
        )
        if (channel) props.onSelect(channel)
      }}
    >
      <SelectTrigger className='w-[320px]'>
        <SelectValue placeholder={t('Select a channel to narrow')} />
      </SelectTrigger>
      <SelectContent>
        {props.target.channels.map((channel) => (
          <SelectItem key={channel.id} value={String(channel.id)}>
            #{channel.id} · {channel.name} · {t('Type')} {channel.type}
            {channel.configured ? ' ✓' : ''}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
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