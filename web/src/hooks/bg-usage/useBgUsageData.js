import { useState, useEffect, useCallback } from 'react';
import { API, showError } from '../../helpers';

const DEFAULT_PAGE = 1;
const DEFAULT_PAGE_SIZE = 20;
const DEFAULT_DAYS = 7;

function getDefaultDates() {
  const end = Math.floor(Date.now() / 1000);
  const start = end - DEFAULT_DAYS * 24 * 3600;
  return { start, end };
}

export function useBgUsageData() {
  const { start: defaultStart, end: defaultEnd } = getDefaultDates();

  const [stats, setStats] = useState(null);
  const [statsLoading, setStatsLoading] = useState(false);

  const [rows, setRows] = useState([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(DEFAULT_PAGE);
  const [pageSize, setPageSize] = useState(DEFAULT_PAGE_SIZE);
  const [tableLoading, setTableLoading] = useState(false);

  // Filters
  const [startTimestamp, setStartTimestamp] = useState(defaultStart);
  const [endTimestamp, setEndTimestamp] = useState(defaultEnd);
  const [model, setModel] = useState('');
  const [granularity, setGranularity] = useState('day');

  const fetchStats = useCallback(async () => {
    setStatsLoading(true);
    try {
      const res = await API.get('/api/bg/usage/stats');
      if (res.data?.success) {
        setStats(res.data.data);
      } else {
        showError(res.data?.message || '获取统计数据失败');
      }
    } catch (e) {
      showError('获取统计数据失败');
    } finally {
      setStatsLoading(false);
    }
  }, []);

  const fetchTable = useCallback(async () => {
    setTableLoading(true);
    try {
      const params = new URLSearchParams({
        start_date: new Date(startTimestamp * 1000).toISOString().slice(0, 10),
        end_date: new Date(endTimestamp * 1000).toISOString().slice(0, 10),
        granularity,
        include_cost: 'true',
        limit: pageSize.toString(),
        offset: ((page - 1) * pageSize).toString(),
      });
      if (model) params.set('model', model);

      const res = await API.get(`/v1/bg/usage?${params}`);
      if (res.data?.success !== false) {
        // The usage API returns an array directly or wrapped
        const data = res.data?.data ?? res.data;
        if (Array.isArray(data)) {
          setRows(data);
          setTotal(data.length);
        } else if (data?.items) {
          setRows(data.items);
          setTotal(data.total ?? data.items.length);
        } else {
          setRows([]);
          setTotal(0);
        }
      } else {
        showError(res.data?.message || '获取用量数据失败');
        setRows([]);
      }
    } catch (e) {
      showError('获取用量数据失败');
      setRows([]);
    } finally {
      setTableLoading(false);
    }
  }, [startTimestamp, endTimestamp, model, granularity, page, pageSize]);

  useEffect(() => {
    fetchStats();
  }, [fetchStats]);

  useEffect(() => {
    fetchTable();
  }, [fetchTable]);

  return {
    // Stats KPI
    stats,
    statsLoading,
    // Table
    rows,
    total,
    tableLoading,
    page,
    pageSize,
    setPage,
    setPageSize: (v) => {
      setPageSize(v);
      setPage(1);
    },
    // Filters
    startTimestamp,
    endTimestamp,
    model,
    granularity,
    setStartTimestamp,
    setEndTimestamp,
    setModel,
    setGranularity,
    // Refresh
    refreshStats: fetchStats,
    refreshTable: fetchTable,
  };
}
