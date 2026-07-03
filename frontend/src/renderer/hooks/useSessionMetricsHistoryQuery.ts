import { useQuery } from "@tanstack/react-query";
import type { components } from "../../api/schema";
import { apiClient, apiErrorMessage } from "../lib/api-client";

type SessionMetricsPoint = components["schemas"]["SessionMetricsPoint"];
type SessionMetricsDetail = components["schemas"]["SessionMetricsDetail"];

export const sessionMetricsQueryKey = (sessionId?: string) =>
	sessionId ? (["session-metrics", sessionId] as const) : (["session-metrics"] as const);

export const sessionMetricsHistoryQueryKey = (sessionId?: string) =>
	sessionId ? (["session-metrics-history", sessionId] as const) : (["session-metrics-history"] as const);

export async function fetchSessionMetrics(sessionId: string): Promise<SessionMetricsDetail | null> {
	const { data, error } = await apiClient.GET("/api/v1/sessions/{sessionId}/metrics", {
		params: { path: { sessionId } },
	});
	if (error) throw new Error(apiErrorMessage(error, "Unable to load session metrics"));
	return data?.metrics ?? null;
}

export async function fetchSessionMetricsHistory(sessionId: string, since?: string): Promise<SessionMetricsPoint[]> {
	const { data, error } = await apiClient.GET("/api/v1/sessions/{sessionId}/metrics/history", {
		params: { path: { sessionId }, query: { since } },
	});
	if (error) throw new Error(apiErrorMessage(error, "Unable to load session metrics history"));
	return data?.metrics ?? [];
}

export function useSessionMetricsQuery(sessionId?: string) {
	return useQuery({
		queryKey: sessionMetricsQueryKey(sessionId),
		enabled: Boolean(sessionId),
		queryFn: () => fetchSessionMetrics(sessionId!),
		refetchInterval: 30_000,
		retry: 1,
	});
}

export function useSessionMetricsHistoryQuery(sessionId?: string, since?: string) {
	return useQuery({
		queryKey: sessionMetricsHistoryQueryKey(sessionId),
		enabled: Boolean(sessionId),
		queryFn: () => fetchSessionMetricsHistory(sessionId!, since),
		refetchInterval: 60_000,
		retry: 1,
	});
}
