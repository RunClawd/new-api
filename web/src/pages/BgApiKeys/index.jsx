import React, { useState, useEffect, useCallback } from 'react';
import {
  Card,
  Table,
  Button,
  Modal,
  Form,
  Select,
  Input,
  Typography,
  Toast,
  Tag,
  Popconfirm,
  Banner,
} from '@douyinfe/semi-ui';
import { IconPlus, IconDelete, IconKey, IconCopy } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, showError } from '../../helpers';

const { Title, Text, Paragraph } = Typography;
const { Option } = Select;

export default function BgApiKeys() {
  const { t } = useTranslation();
  const [keys, setKeys] = useState([]);
  const [projects, setProjects] = useState([]);
  const [loading, setLoading] = useState(false);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize] = useState(10);
  const [filterProjectId, setFilterProjectId] = useState('');
  const [createVisible, setCreateVisible] = useState(false);
  const [revealedKey, setRevealedKey] = useState('');
  const [revealModalVisible, setRevealModalVisible] = useState(false);
  const [newKeyName, setNewKeyName] = useState('');
  const [newKeyProjectId, setNewKeyProjectId] = useState('');

  const fetchKeys = useCallback(async () => {
    setLoading(true);
    try {
      let url = `/api/bg/dev/apikeys?p=${page - 1}&size=${pageSize}`;
      if (filterProjectId) {
        url += `&project_id=${filterProjectId}`;
      }
      const res = await API.get(url);
      if (res.data?.success) {
        const data = res.data.data;
        setKeys(data.items || []);
        setTotal(data.total || 0);
      }
    } catch {
      showError(t('加载 API 密钥失败'));
    }
    setLoading(false);
  }, [page, pageSize, filterProjectId, t]);

  const fetchProjects = useCallback(async () => {
    try {
      const res = await API.get('/api/bg/dev/projects?size=100');
      if (res.data?.success) {
        setProjects(res.data.data?.items || []);
      }
    } catch {
      // ignore
    }
  }, []);

  useEffect(() => {
    fetchKeys();
  }, [fetchKeys]);

  useEffect(() => {
    fetchProjects();
  }, [fetchProjects]);

  const handleCreate = async () => {
    if (!newKeyName.trim()) {
      Toast.warning(t('请输入名称'));
      return;
    }
    try {
      const body = { name: newKeyName };
      if (newKeyProjectId) body.project_id = newKeyProjectId;
      const res = await API.post('/api/bg/dev/apikeys', body);
      if (res.data?.success) {
        const keyData = res.data.data;
        setRevealedKey(keyData.key);
        setRevealModalVisible(true);
        setCreateVisible(false);
        setNewKeyName('');
        setNewKeyProjectId('');
        fetchKeys();
        Toast.success(t('创建成功'));
      } else {
        Toast.error(res.data?.message || t('创建失败'));
      }
    } catch {
      Toast.error(t('创建失败'));
    }
  };

  const handleDelete = async (id) => {
    try {
      const res = await API.delete(`/api/bg/dev/apikeys/${id}`);
      if (res.data?.success) {
        Toast.success(t('删除成功'));
        fetchKeys();
      } else {
        Toast.error(res.data?.message || t('删除失败'));
      }
    } catch {
      Toast.error(t('删除失败'));
    }
  };

  const handleReveal = async (id) => {
    try {
      const res = await API.post(`/api/bg/dev/apikeys/${id}/reveal`);
      if (res.data?.success) {
        setRevealedKey(res.data.data.key);
        setRevealModalVisible(true);
      } else {
        Toast.error(res.data?.message || t('获取密钥失败'));
      }
    } catch {
      Toast.error(t('获取密钥失败'));
    }
  };

  const copyToClipboard = (text) => {
    navigator.clipboard.writeText(text).then(() => {
      Toast.success(t('已复制到剪贴板'));
    });
  };

  const columns = [
    { title: 'ID', dataIndex: 'id', width: 60 },
    { title: t('名称'), dataIndex: 'name', width: 200 },
    {
      title: t('密钥'),
      dataIndex: 'key',
      width: 250,
      render: (text) => <Text copyable={{ content: text }}>{text}</Text>,
    },
    {
      title: t('关联项目'),
      dataIndex: 'bg_project_id',
      width: 120,
      render: (val) =>
        val > 0 ? (
          <Tag color='blue'>
            {projects.find((p) => p.id === val)?.project_id || `#${val}`}
          </Tag>
        ) : (
          <Tag color='grey'>{t('未绑定')}</Tag>
        ),
    },
    {
      title: t('状态'),
      dataIndex: 'status',
      width: 80,
      render: (v) => (
        <Tag color={v === 1 ? 'green' : 'red'}>
          {v === 1 ? t('启用') : t('禁用')}
        </Tag>
      ),
    },
    {
      title: t('操作'),
      width: 180,
      render: (_, record) => (
        <div style={{ display: 'flex', gap: 8 }}>
          <Button
            icon={<IconKey />}
            size='small'
            onClick={() => handleReveal(record.id)}
          >
            {t('查看')}
          </Button>
          <Popconfirm
            title={t('确定删除此密钥？')}
            onConfirm={() => handleDelete(record.id)}
          >
            <Button icon={<IconDelete />} size='small' type='danger'>
              {t('删除')}
            </Button>
          </Popconfirm>
        </div>
      ),
    },
  ];

  return (
    <div style={{ padding: '24px', maxWidth: 1200, margin: '60px auto 0' }}>
      <div
        style={{
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          marginBottom: 16,
        }}
      >
        <div>
          <Title heading={4} style={{ margin: 0 }}>{t('API 密钥管理')}</Title>
          <Text type='tertiary' size='small'>{t('管理您的 BaseGate API 密钥')}</Text>
        </div>
        <div style={{ display: 'flex', gap: 12 }}>
          <Select
            placeholder={t('按项目过滤')}
            value={filterProjectId}
            onChange={(v) => {
              setFilterProjectId(v || '');
              setPage(1);
            }}
            showClear
            style={{ width: 200 }}
          >
            {projects.map((p) => (
              <Option key={p.project_id} value={p.project_id}>
                {p.name} ({p.project_id})
              </Option>
            ))}
          </Select>
          <Button
            icon={<IconPlus />}
            theme='solid'
            onClick={() => setCreateVisible(true)}
          >
            {t('创建密钥')}
          </Button>
        </div>
      </div>

      <Card shadows='hover' style={{ borderRadius: 12 }}>
        <Table
          columns={columns}
          dataSource={keys}
          loading={loading}
          pagination={{
            currentPage: page,
            pageSize,
            total,
            onPageChange: setPage,
          }}
          rowKey='id'
        />
      </Card>

      {/* Create modal */}
      <Modal
        title={t('创建 API 密钥')}
        visible={createVisible}
        onOk={handleCreate}
        onCancel={() => setCreateVisible(false)}
        okText={t('创建')}
        cancelText={t('取消')}
      >
        <Form layout='vertical'>
          <Form.Slot label={t('名称')}>
            <Input
              value={newKeyName}
              onChange={setNewKeyName}
              placeholder={t('输入密钥名称')}
            />
          </Form.Slot>
          <Form.Slot label={t('关联项目（可选）')}>
            <Select
              value={newKeyProjectId}
              onChange={setNewKeyProjectId}
              showClear
              placeholder={t('选择项目')}
              style={{ width: '100%' }}
            >
              {projects.map((p) => (
                <Option key={p.project_id} value={p.project_id}>
                  {p.name} ({p.project_id})
                </Option>
              ))}
            </Select>
          </Form.Slot>
        </Form>
      </Modal>

      {/* Reveal key modal */}
      <Modal
        title={t('API 密钥')}
        visible={revealModalVisible}
        onOk={() => setRevealModalVisible(false)}
        onCancel={() => setRevealModalVisible(false)}
        footer={
          <Button onClick={() => setRevealModalVisible(false)}>{t('关闭')}</Button>
        }
      >
        <Banner
          type='warning'
          description={t('请妥善保管此密钥，关闭后需通过「查看」按钮获取。')}
          style={{ marginBottom: 16 }}
        />
        <div
          style={{
            background: 'var(--semi-color-fill-0)',
            padding: '12px 16px',
            borderRadius: 8,
            display: 'flex',
            alignItems: 'center',
            gap: 8,
          }}
        >
          <Paragraph
            copyable={{ content: revealedKey }}
            style={{ margin: 0, flex: 1, wordBreak: 'break-all' }}
          >
            {revealedKey}
          </Paragraph>
          <Button
            icon={<IconCopy />}
            size='small'
            onClick={() => copyToClipboard(revealedKey)}
          />
        </div>
      </Modal>
    </div>
  );
}
