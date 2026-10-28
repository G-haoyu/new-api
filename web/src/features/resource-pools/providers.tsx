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
import type { ResourcePoolProvider } from "./api";

export type ProviderMeta = {
  key: ResourcePoolProvider;
  label: string;
  color: string;
  icon: React.ReactNode;
};

const stroke = {
  fill: "none",
  stroke: "#fff",
  strokeWidth: 2,
  strokeLinecap: "round",
  strokeLinejoin: "round",
} as const;

export const PROVIDERS: ProviderMeta[] = [
  {
    key: "claude",
    label: "Claude",
    color: "#D97757",
    icon: (
      <svg viewBox="0 0 24 24" width="16" height="16" {...stroke}>
        <path d="M12 3v18M3 12h18M5.6 5.6l12.8 12.8M18.4 5.6L5.6 18.4" />
      </svg>
    ),
  },
  {
    key: "gpt",
    label: "GPT",
    color: "#10A37F",
    icon: (
      <svg viewBox="0 0 24 24" width="16" height="16" {...stroke}>
        <path d="M4 5a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H9l-5 4V5z" />
      </svg>
    ),
  },
  {
    key: "gemini",
    label: "Gemini",
    color: "#1B72E8",
    icon: (
      <svg viewBox="0 0 24 24" width="16" height="16" {...stroke}>
        <path d="M12 3l1.8 5.2L19 10l-5.2 1.8L12 17l-1.8-5.2L5 10l5.2-1.8L12 3z" />
      </svg>
    ),
  },
  {
    key: "grok",
    label: "Grok",
    color: "#111111",
    icon: (
      <svg viewBox="0 0 24 24" width="16" height="16" {...stroke}>
        <path d="M5 5l14 14M19 5L5 19" />
      </svg>
    ),
  },
];
