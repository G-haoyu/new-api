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
import { api } from "@/lib/api";
import { requireServerSuccess } from "@/lib/server-error-message";

export type ResourcePoolProvider = "claude" | "gpt" | "gemini" | "grok";

export type ResourcePoolSource = {
  id: number;
  provider: ResourcePoolProvider;
  name: string;
  endpoint: string;
  admin_password: string;
  group_name: string;
  enabled: boolean;
  snapshot_at: number;
};

export type ResourcePoolAccount = {
  id: string;
  email: string;
  user_id?: string;
  name?: string;
  enabled: boolean;
  subscription_type?: string;
  subscription_title?: string;
  usage_current: number;
  usage_limit: number;
  usage_percent?: number;
  usage_percentage: number;
  next_reset_date?: string;
  groups?: string[];
  source_group?: string;
  source_channel?: string;
  rpm_limit?: number;
  rpm_current?: number;
  last_refresh?: number;
  source_id: number;
  source_name: string;
  source_endpoint: string;
  // codex2api (Grok) fields
  plan_type?: string;
  plan_display?: string;
  status?: string;
  models?: string[];
  total_requests?: number;
  usage_percent_7d?: number;
  usage_percent_5h?: number;
  weekly_percent?: number;
  requests_7d?: number;
  tokens_7d?: number;
  account_billed_7d?: number;
  reset_7d_at?: string;
  reset_5h_at?: string;
};

export type ResourcePoolSourceStatus = {
  source_id: number;
  source_name: string;
  source_endpoint: string;
  status: string;
  count: number;
  error: string;
};

export type ResourcePoolUsage = {
  accounts: ResourcePoolAccount[];
  sources: ResourcePoolSourceStatus[];
  /** Unix seconds of the upstream poll; the server caches polls briefly. */
  refreshed_at: number;
};

export async function getResourcePoolSources(
  provider: ResourcePoolProvider,
): Promise<ResourcePoolSource[]> {
  const response = await api.get<{
    success: boolean;
    data: ResourcePoolSource[];
  }>("/api/channel/resource_pools/sources", { params: { provider } });
  return requireServerSuccess(response.data).data ?? [];
}

export async function saveResourcePoolSources(
  provider: ResourcePoolProvider,
  data: Omit<ResourcePoolSource, "provider" | "snapshot_at">[],
): Promise<void> {
  const response = await api.post<{ success: boolean }>(
    "/api/channel/resource_pools/sources",
    { provider, data },
  );
  requireServerSuccess(response.data);
}

/** `refresh` bypasses the server-side poll cache. */
export async function getResourcePoolUsage(
  provider: ResourcePoolProvider,
  refresh = false,
): Promise<ResourcePoolUsage> {
  const response = await api.get<{
    success: boolean;
    data: ResourcePoolUsage;
  }>("/api/channel/resource_pools/usage", {
    params: refresh ? { provider, refresh: true } : { provider },
  });
  return requireServerSuccess(response.data).data;
}

export async function deleteResourcePoolSource(id: number): Promise<void> {
  const response = await api.delete<{ success: boolean }>(
    `/api/channel/resource_pools/sources/${id}`,
  );
  requireServerSuccess(response.data);
}
