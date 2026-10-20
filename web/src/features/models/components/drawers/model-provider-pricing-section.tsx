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
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2, Trash2 } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { getLobeIcon } from '@/lib/lobe-icon'
import { invalidateModelSquareQueries } from '@/features/pricing/lib/query-keys'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

import {
  createModelProviderPrice,
  deleteModelProviderPrice,
  getAllProviders,
  getModelProviderPrices,
  updateModelProviderPrice,
} from '../../api'
import {
  modelProviderPriceQueryKeys,
  providerModelQueryKeys,
  providersQueryKeys,
} from '../../lib'
import type { ModelProviderPrice, Provider } from '../../types'

type DraftPrice = {
  id?: number
  inputPrice: string
  outputPrice: string
  cacheReadPrice: string
  cacheWritePrice: string
  inputListPrice: string
  outputListPrice: string
  cacheReadListPrice: string
  cacheWriteListPrice: string
  discountRate: string
  sourceUrl: string
  providerModelName: string
  contextLength: string
  maxOutputTokens: string
  region: string
  precision: string
  quantization: string
  supportedParameters: string
  streamCancellation: CapabilityDraft
  free: CapabilityDraft
  batch: CapabilityDraft
  effectiveAt: string
}

type CapabilityDraft = 'unknown' | 'supported' | 'unsupported'

function capabilityToDraft(value?: boolean | null): CapabilityDraft {
  if (value == null) return 'unknown'
  return value ? 'supported' : 'unsupported'
}

function capabilityToPayload(value: CapabilityDraft): boolean | undefined {
  if (value === 'unknown') return undefined
  return value === 'supported'
}

function timestampToLocalInput(value?: number): string {
  if (!value) return ''
  const date = new Date(value * 1000)
  const local = new Date(date.getTime() - date.getTimezoneOffset() * 60_000)
  return local.toISOString().slice(0, 16)
}

function supportedParametersToDraft(value?: string): string {
  if (!value) return ''
  try {
    const parsed = JSON.parse(value)
    return Array.isArray(parsed) ? parsed.join(', ') : value
  } catch {
    return value
  }
}

function priceToDraft(price?: ModelProviderPrice): DraftPrice {
  return {
    id: price?.id,
    inputPrice: price?.input_price == null ? '' : String(price.input_price),
    outputPrice: price?.output_price == null ? '' : String(price.output_price),
    cacheReadPrice:
      price?.cache_read_price == null ? '' : String(price.cache_read_price),
    cacheWritePrice:
      price?.cache_write_price == null ? '' : String(price.cache_write_price),
    inputListPrice:
      price?.input_list_price == null ? '' : String(price.input_list_price),
    outputListPrice:
      price?.output_list_price == null ? '' : String(price.output_list_price),
    cacheReadListPrice:
      price?.cache_read_list_price == null
        ? ''
        : String(price.cache_read_list_price),
    cacheWriteListPrice:
      price?.cache_write_list_price == null
        ? ''
        : String(price.cache_write_list_price),
    discountRate:
      price?.discount_rate == null ? '' : String(price.discount_rate),
    sourceUrl: price?.source_url || '',
    providerModelName: price?.model_name || '',
    contextLength:
      price?.context_length == null ? '' : String(price.context_length),
    maxOutputTokens:
      price?.max_output_tokens == null ? '' : String(price.max_output_tokens),
    region: price?.region || '',
    precision: price?.precision || '',
    quantization: price?.quantization || '',
    supportedParameters: supportedParametersToDraft(
      price?.supported_parameters
    ),
    streamCancellation: capabilityToDraft(price?.stream_cancellation),
    free: capabilityToDraft(price?.free),
    batch: capabilityToDraft(price?.batch),
    effectiveAt: timestampToLocalInput(price?.effective_at),
  }
}

function getEffectivePriceValue(
  currentPrice: string,
  listPrice: string,
  discountRate: string
): string {
  if (!listPrice.trim()) return currentPrice
  const parsedListPrice = Number.parseFloat(listPrice)
  const parsedRate = discountRate.trim() ? Number.parseFloat(discountRate) : 100
  if (
    !Number.isFinite(parsedListPrice) ||
    parsedListPrice < 0 ||
    !Number.isFinite(parsedRate) ||
    parsedRate < 0 ||
    parsedRate > 100
  ) {
    return currentPrice
  }
  return (parsedListPrice * (parsedRate / 100)).toFixed(12).replace(/\.0+$/, '').replace(/(\.\d*?)0+$/, '$1')
}

export function ModelProviderPricingSection(props: {
  modelId?: number
  enabled: boolean
  providerSlug?: string
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [drafts, setDrafts] = useState<Record<string, DraftPrice>>({})
  const [dirtySlugs, setDirtySlugs] = useState<Set<string>>(new Set())
  const [savingSlug, setSavingSlug] = useState<string | null>(null)
  const activeModelId = useRef<number | undefined>(props.modelId)
  const activeProviderSlug = useRef<string | undefined>(props.providerSlug)

  const providersQuery = useQuery({
    queryKey: providersQueryKeys.list({ all: true }),
    queryFn: getAllProviders,
    enabled: props.enabled,
    staleTime: 0,
    refetchOnMount: 'always',
  })
  const pricesQuery = useQuery({
    queryKey: modelProviderPriceQueryKeys.list(props.modelId ?? 0),
    queryFn: () => getModelProviderPrices(props.modelId ?? 0),
    enabled: props.enabled && Boolean(props.modelId),
    staleTime: 0,
    refetchOnMount: 'always',
  })

  const providers = useMemo(() => {
    const items = providersQuery.data ?? []
    if (props.providerSlug) {
      return items.filter((provider) => provider.slug === props.providerSlug)
    }
    return items.filter((provider) => provider.status === 1)
  }, [props.providerSlug, providersQuery.data])

  useEffect(() => {
    if (activeModelId.current === props.modelId) return
    activeModelId.current = props.modelId
    setDrafts({})
    setDirtySlugs(new Set())
  }, [props.modelId])

  useEffect(() => {
    if (activeProviderSlug.current === props.providerSlug) return
    activeProviderSlug.current = props.providerSlug
    setDrafts({})
    setDirtySlugs(new Set())
  }, [props.providerSlug])

  useEffect(() => {
    const prices = pricesQuery.data?.data
    if (!prices) return
    const serverSlugs = new Set(prices.map((price) => price.provider_slug))
    const pricesBySlug = new Map(
      prices.map((price) => [price.provider_slug, price])
    )
    setDrafts((current) => {
      const next = { ...current }
      for (const [slug, price] of pricesBySlug) {
        if (!dirtySlugs.has(slug)) {
          next[slug] = priceToDraft(price)
        }
      }
      for (const [slug, draft] of Object.entries(current)) {
        if (!dirtySlugs.has(slug) && draft.id && !serverSlugs.has(slug)) {
          next[slug] = priceToDraft()
        }
      }
      return next
    })
  }, [dirtySlugs, pricesQuery.data])

  if (!props.modelId) {
    return (
      <div className='text-muted-foreground rounded-lg border border-dashed px-3 py-3 text-sm'>
        {t('Save the model first to configure provider prices.')}
      </div>
    )
  }
  const modelId = props.modelId

  const updateDraft = (
    provider: Provider,
    field: keyof DraftPrice,
    value: DraftPrice[keyof DraftPrice]
  ) => {
    setDrafts((current) => ({
      ...current,
      [provider.slug]: {
        ...priceToDraft(),
        ...current[provider.slug],
        [field]: value,
      },
    }))
    setDirtySlugs((current) => {
      const next = new Set(current)
      next.add(provider.slug)
      return next
    })
  }

  const handleSave = async (provider: Provider) => {
    const draft = drafts[provider.slug] ?? priceToDraft()
    const inputPrice = Number.parseFloat(draft.inputPrice)
    const outputPrice = Number.parseFloat(draft.outputPrice)

    const parseListPrice = (value: string) => {
      if (!value.trim()) return undefined
      const parsed = Number.parseFloat(value)
      return Number.isFinite(parsed) && parsed >= 0 ? parsed : null
    }
    const inputListPrice = parseListPrice(draft.inputListPrice)
    const outputListPrice = parseListPrice(draft.outputListPrice)
    const cacheReadListPrice = parseListPrice(draft.cacheReadListPrice)
    const cacheWriteListPrice = parseListPrice(draft.cacheWriteListPrice)
    if (
      inputListPrice === null ||
      outputListPrice === null ||
      cacheReadListPrice === null ||
      cacheWriteListPrice === null
    ) {
      toast.error(t('List prices must be non-negative numbers'))
      return
    }
    const discountRate = parseListPrice(draft.discountRate)
    if (discountRate === null || (discountRate ?? 100) > 100) {
      toast.error(t('Discount rate must be between 0 and 100'))
      return
    }
    if (
      (discountRate ?? 100) < 100 &&
      inputListPrice == null &&
      outputListPrice == null &&
      cacheReadListPrice == null &&
      cacheWriteListPrice == null
    ) {
      toast.error(t('Discount rate requires at least one list price'))
      return
    }
    const hasInputListPrice = inputListPrice != null
    const hasOutputListPrice = outputListPrice != null
    if (
      (!hasInputListPrice &&
        (!Number.isFinite(inputPrice) || inputPrice < 0)) ||
      (!hasOutputListPrice &&
        (!Number.isFinite(outputPrice) || outputPrice < 0))
    ) {
      toast.error(t('Input and output prices must be non-negative numbers'))
      return
    }

    const parseOptional = (value: string) => {
      if (!value.trim()) return undefined
      const parsed = Number.parseFloat(value)
      return Number.isFinite(parsed) && parsed >= 0 ? parsed : null
    }
    const cacheReadPrice = parseOptional(draft.cacheReadPrice)
    const cacheWritePrice = parseOptional(draft.cacheWritePrice)
    if (cacheReadPrice === null || cacheWritePrice === null) {
      toast.error(t('Cache prices must be non-negative numbers'))
      return
    }
    const parseOptionalInteger = (value: string) => {
      if (!value.trim()) return undefined
      const parsed = Number.parseInt(value, 10)
      return Number.isInteger(parsed) && parsed >= 0 ? parsed : null
    }
    const contextLength = parseOptionalInteger(draft.contextLength)
    const maxOutputTokens = parseOptionalInteger(draft.maxOutputTokens)
    if (contextLength === null || maxOutputTokens === null) {
      toast.error(t('Context length and max output tokens must be non-negative integers'))
      return
    }
    const supportedParameters = draft.supportedParameters
      .split(/[,;\n]+/)
      .map((value) => value.trim())
      .filter(Boolean)
    const effectiveAt = draft.effectiveAt
      ? Math.floor(new Date(draft.effectiveAt).getTime() / 1000)
      : undefined
    if (effectiveAt != null && !Number.isFinite(effectiveAt)) {
      toast.error(t('Effective time must be a valid date'))
      return
    }

    setSavingSlug(provider.slug)
    try {
      const payload = {
        model_id: modelId,
        provider_slug: provider.slug,
        input_price: Number.isFinite(inputPrice) ? inputPrice : 0,
        output_price: Number.isFinite(outputPrice) ? outputPrice : 0,
        cache_read_price: cacheReadPrice,
        cache_write_price: cacheWritePrice,
        input_list_price: inputListPrice,
        output_list_price: outputListPrice,
        cache_read_list_price: cacheReadListPrice,
        cache_write_list_price: cacheWriteListPrice,
        discount_rate: discountRate,
        source_url: draft.sourceUrl.trim(),
        model_name: draft.providerModelName.trim(),
        context_length: contextLength,
        max_output_tokens: maxOutputTokens,
        region: draft.region.trim(),
        precision: draft.precision.trim(),
        quantization: draft.quantization.trim(),
        supported_parameters:
          supportedParameters.length > 0
            ? JSON.stringify(supportedParameters)
            : undefined,
        stream_cancellation:
          capabilityToPayload(draft.streamCancellation),
        free: capabilityToPayload(draft.free),
        batch: capabilityToPayload(draft.batch),
        effective_at: effectiveAt,
      }
      const response = draft.id
        ? await updateModelProviderPrice({ ...payload, id: draft.id })
        : await createModelProviderPrice(payload)
      if (!response.success) throw new Error(response.message || t('Operation failed'))
      if (response.data) {
        setDrafts((current) => ({
          ...current,
          [provider.slug]: priceToDraft(response.data),
        }))
      }
      setDirtySlugs((current) => {
        const next = new Set(current)
        next.delete(provider.slug)
        return next
      })
      toast.success(t('Provider price saved successfully'))
      queryClient.invalidateQueries({ queryKey: modelProviderPriceQueryKeys.list(modelId) })
      queryClient.invalidateQueries({ queryKey: providerModelQueryKeys.all })
      await invalidateModelSquareQueries(queryClient)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('Operation failed'))
    } finally {
      setSavingSlug(null)
    }
  }

  const handleDelete = async (provider: Provider) => {
    const draft = drafts[provider.slug]
    if (!draft?.id || !window.confirm(t('Delete this provider price?'))) return
    setSavingSlug(provider.slug)
    try {
      const response = await deleteModelProviderPrice(draft.id)
      if (!response.success) throw new Error(response.message || t('Operation failed'))
      toast.success(t('Provider price deleted successfully'))
      setDrafts((current) => ({ ...current, [provider.slug]: priceToDraft() }))
      setDirtySlugs((current) => {
        const next = new Set(current)
        next.delete(provider.slug)
        return next
      })
      queryClient.invalidateQueries({ queryKey: modelProviderPriceQueryKeys.list(modelId) })
      queryClient.invalidateQueries({ queryKey: providerModelQueryKeys.all })
      await invalidateModelSquareQueries(queryClient)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('Operation failed'))
    } finally {
      setSavingSlug(null)
    }
  }

  return (
    <div className='space-y-3'>
      <p className='text-muted-foreground text-xs'>
        {t('Prices are displayed in USD per 1M tokens and override the model default price for this provider.')}
      </p>
      {providers.length === 0 ? (
        <div className='text-muted-foreground rounded-lg border border-dashed px-3 py-3 text-sm'>
          {t('No providers available')}
        </div>
      ) : providers.map((provider) => {
        const draft = drafts[provider.slug] ?? priceToDraft()
        const icon = provider.icon ? getLobeIcon(provider.icon, 20) : null
        return (
          <div key={provider.slug} className='space-y-3 rounded-lg border p-3'>
            <div className='flex items-center justify-between gap-3'>
              <div className='flex min-w-0 items-center gap-2'>
                <div className='bg-muted flex size-8 shrink-0 items-center justify-center rounded-md'>
                  {icon || <span className='text-muted-foreground text-xs'>{provider.display_name.charAt(0)}</span>}
                </div>
                <div className='min-w-0'>
                  <div className='truncate text-sm font-medium'>{provider.display_name}</div>
                  <div className='text-muted-foreground truncate font-mono text-xs'>{provider.slug}</div>
                </div>
              </div>
              <div className='flex shrink-0 gap-1'>
                <Button type='button' size='sm' onClick={() => void handleSave(provider)} disabled={savingSlug === provider.slug}>
                  {savingSlug === provider.slug && <Loader2 className='size-3.5 animate-spin' />}
                  {t('Save')}
                </Button>
                {draft.id && <Button type='button' variant='ghost' size='icon' aria-label={t('Delete')} onClick={() => void handleDelete(provider)} disabled={savingSlug === provider.slug}><Trash2 className='size-4' /></Button>}
              </div>
            </div>
            <div className='border-border/70 space-y-3 border-t pt-3'>
              <div>
                <h4 className='text-sm font-semibold'>{t('Standard price and discount')}</h4>
                <p className='text-muted-foreground mt-1 text-xs'>
                  {t('Configure standard Provider prices and a discount rate for model square display. 90 means 90% of the standard price (9-fold).')}
                </p>
              </div>
              <div className='grid gap-3 sm:grid-cols-2'>
                {([
                  ['inputListPrice', t('Input list price ($/1M)')],
                  ['outputListPrice', t('Output list price ($/1M)')],
                  ['cacheReadListPrice', t('Cache read list price ($/1M)')],
                  ['cacheWriteListPrice', t('Cache write list price ($/1M)')],
                ] as const).map(([field, label]) => (
                  <label key={field} className='space-y-1'>
                    <span className='text-muted-foreground text-xs'>{label}</span>
                    <Input
                      type='number'
                      min='0'
                      step='any'
                      value={draft[field]}
                      onChange={(event) => updateDraft(provider, field, event.target.value)}
                      placeholder={t('Optional')}
                    />
                  </label>
                ))}
                <label className='space-y-1'>
                  <span className='text-muted-foreground text-xs'>{t('Discount rate (%)')}</span>
                  <Input
                    type='number'
                    min='0'
                    max='100'
                    step='any'
                    value={draft.discountRate}
                    onChange={(event) => updateDraft(provider, 'discountRate', event.target.value)}
                    placeholder='100'
                  />
                </label>
              </div>
            </div>
            <div className='border-border/70 space-y-3 border-t pt-3'>
              <div>
                <h4 className='text-sm font-semibold'>{t('Current effective price')}</h4>
                <p className='text-muted-foreground mt-1 text-xs'>
                  {t('Calculated automatically from the standard price and discount rate. Leave the standard price empty to keep the legacy price.')}
                </p>
              </div>
              <div className='grid gap-3 sm:grid-cols-2'>
                {([
                  ['inputPrice', 'inputListPrice', t('Current input price ($/1M)')],
                  ['outputPrice', 'outputListPrice', t('Current output price ($/1M)')],
                  ['cacheReadPrice', 'cacheReadListPrice', t('Current cache read price ($/1M)')],
                  ['cacheWritePrice', 'cacheWriteListPrice', t('Current cache write price ($/1M)')],
                ] as const).map(([field, listField, label]) => {
                  const value = getEffectivePriceValue(
                    draft[field],
                    draft[listField],
                    draft.discountRate
                  )
                  const isCalculated = draft[listField].trim().length > 0
                  return (
                    <label key={field} className='space-y-1'>
                      <span className='text-muted-foreground text-xs'>{label}</span>
                      <Input
                        type='number'
                        min='0'
                        step='any'
                        value={value}
                        readOnly={isCalculated}
                        onChange={(event) => updateDraft(provider, field, event.target.value)}
                        placeholder={t('To be added')}
                      />
                    </label>
                  )
                })}
              </div>
            </div>
            <div className='border-border/70 space-y-3 border-t pt-3'>
              <div>
                <h4 className='text-sm font-semibold'>{t('Provider metadata')}</h4>
                <p className='text-muted-foreground mt-1 text-xs'>
                  {t('Optional metadata used for model details and filtering.')}
                </p>
              </div>
              <div className='grid gap-3 sm:grid-cols-2'>
                {([
                  ['providerModelName', t('Provider model name')],
                  ['region', t('Region')],
                  ['precision', t('Precision')],
                  ['quantization', t('Quantization')],
                  ['contextLength', t('Context length')],
                  ['maxOutputTokens', t('Max output tokens')],
                ] as const).map(([field, label]) => (
                  <label key={field} className='space-y-1'>
                    <span className='text-muted-foreground text-xs'>{label}</span>
                    <Input
                      type={field === 'contextLength' || field === 'maxOutputTokens' ? 'number' : 'text'}
                      min={field === 'contextLength' || field === 'maxOutputTokens' ? 0 : undefined}
                      step={field === 'contextLength' || field === 'maxOutputTokens' ? 1 : undefined}
                      value={draft[field]}
                      onChange={(event) => updateDraft(provider, field, event.target.value)}
                      placeholder={t('To be added')}
                    />
                  </label>
                ))}
              </div>
              <label className='block space-y-1'>
                <span className='text-muted-foreground text-xs'>{t('Supported parameters')}</span>
                <Textarea
                  rows={2}
                  value={draft.supportedParameters}
                  onChange={(event) => updateDraft(provider, 'supportedParameters', event.target.value)}
                  placeholder='temperature, top_p, tools'
                />
                <span className='text-muted-foreground text-[11px]'>
                  {t('Separate parameter names with commas or new lines.')}
                </span>
              </label>
              <div className='grid gap-3 sm:grid-cols-3'>
                {([
                  ['streamCancellation', t('Stream cancellation support')],
                  ['free', t('Free capability')],
                  ['batch', t('Batch capability')],
                ] as const).map(([field, label]) => (
                  <label key={field} className='space-y-1'>
                    <span className='text-muted-foreground text-xs'>{label}</span>
                    <Select
                      value={draft[field]}
                      onValueChange={(value) =>
                        updateDraft(provider, field, value as CapabilityDraft)
                      }
                    >
                      <SelectTrigger className='w-full'>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent alignItemWithTrigger={false}>
                        <SelectItem value='unknown'>{t('Not configured')}</SelectItem>
                        <SelectItem value='supported'>{t('Supported')}</SelectItem>
                        <SelectItem value='unsupported'>{t('Not supported')}</SelectItem>
                      </SelectContent>
                    </Select>
                  </label>
                ))}
              </div>
              <label className='block space-y-1'>
                <span className='text-muted-foreground text-xs'>{t('Effective time')}</span>
                <Input
                  type='datetime-local'
                  value={draft.effectiveAt}
                  onChange={(event) => updateDraft(provider, 'effectiveAt', event.target.value)}
                />
              </label>
            </div>
            <label className='block space-y-1'>
              <span className='text-muted-foreground text-xs'>{t('Source URL')}</span>
              <Input value={draft.sourceUrl} onChange={(event) => updateDraft(provider, 'sourceUrl', event.target.value)} />
            </label>
          </div>
        )
      })}
    </div>
  )
}
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
