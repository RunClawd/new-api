import React, { useState, useEffect, useCallback } from 'react';
import {
  Card,
  Table,
  Typography,
  Toast,
  Tag,
  Tabs,
  TabPane,
  Button,
  Modal,
  Form,
  Input,
  Select,
  InputNumber,
  Popconfirm,
  Switch,
  Space,
} from '@douyinfe/semi-ui';
import { IconPlus, IconRefresh, IconDelete, IconEdit } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, showError } from '../../helpers';

const { Title, Text } = Typography;

const SCOPE_COLORS = {
  platform: 'grey',
  org: 'blue',
  project: 'green',
  apikey: 'orange',
};

const SCOPE_OPTIONS = ['platform', 'org', 'project', 'apikey'];
const ACTION_OPTIONS = ['allow', 'deny'];
const STRATEGY_OPTIONS = ['weighted', 'fixed', 'primary_backup', 'byo_first'];

// ---------------------------------------------------------------------------
// Capability Policies Tab
// ---------------------------------------------------------------------------

function CapabilityPolicies({ t }) {
  const [data, setData] = useState([]);
  const [loading, setLoading] = useState(false);
  const [modalVisible, setModalVisible] = useState(false);
  const [editing, setEditing] = useState(null);
  const [form, setForm] = useState({});

  const fetch = useCallback(async () => {
    setLoading(true);
    try {
      const res = await API.get('/api/bg/policies/capabilities?size=200');
      if (res.data?.success) setData(res.data.data?.items || []);
    } catch {
      showError(t('加载能力策略失败'));
    }
    setLoading(false);
  }, [t]);

  useEffect(() => { fetch(); }, [fetch]);

  const openCreate = () => {
    setEditing(null);
    setForm({ scope: 'platform', scope_id: 0, capability_pattern: '', action: 'allow', priority: 0, description: '', enforced: false });
    setModalVisible(true);
  };

  const openEdit = (row) => {
    setEditing(row);
    setForm({ ...row });
    setModalVisible(true);
  };

  const handleSave = async () => {
    if (!form.capability_pattern?.trim()) {
      Toast.warning(t('请输入能力匹配模式'));
      return;
    }
    try {
      const payload = { ...form };
      let res;
      if (editing) {
        res = await API.put(`/api/bg/policies/capabilities/${editing.id}`, payload);
      } else {
        res = await API.post('/api/bg/policies/capabilities', payload);
      }
      if (res.data?.success) {
        Toast.success(editing ? t('更新成功') : t('创建成功'));
        setModalVisible(false);
        fetch();
      } else {
        Toast.error(res.data?.message || t('操作失败'));
      }
    } catch {
      Toast.error(t('操作失败'));
    }
  };

  const handleDelete = async (id) => {
    try {
      const res = await API.delete(`/api/bg/policies/capabilities/${id}`);
      if (res.data?.success) {
        Toast.success(t('删除成功'));
        fetch();
      } else {
        Toast.error(res.data?.message || t('删除失败'));
      }
    } catch {
      Toast.error(t('删除失败'));
    }
  };

  const columns = [
    { title: 'ID', dataIndex: 'id', width: 60 },
    {
      title: t('能力匹配'),
      dataIndex: 'capability_pattern',
      width: 220,
      render: (v) => <Text code style={{ fontSize: 12 }}>{v}</Text>,
    },
    {
      title: t('作用域'),
      dataIndex: 'scope',
      width: 100,
      render: (v) => <Tag color={SCOPE_COLORS[v] || 'grey'}>{v}</Tag>,
    },
    { title: t('Scope ID'), dataIndex: 'scope_id', width: 80 },
    {
      title: t('行为'),
      dataIndex: 'action',
      width: 80,
      render: (v) => <Tag color={v === 'allow' ? 'green' : 'red'}>{v}</Tag>,
    },
    { title: t('优先级'), dataIndex: 'priority', width: 80 },
    {
      title: t('强制'),
      dataIndex: 'enforced',
      width: 60,
      render: (v) => v ? <Tag color='red'>{t('是')}</Tag> : '-',
    },
    {
      title: t('操作'),
      width: 140,
      render: (_, row) => (
        <Space>
          <Button icon={<IconEdit />} size='small' onClick={() => openEdit(row)} />
          <Popconfirm title={t('确定删除？')} onConfirm={() => handleDelete(row.id)}>
            <Button icon={<IconDelete />} size='small' type='danger' />
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <>
      <Card
        style={{ marginTop: 12 }}
        headerExtraContent={
          <Space>
            <Button icon={<IconPlus />} theme='solid' size='small' onClick={openCreate}>{t('新建')}</Button>
            <Button icon={<IconRefresh />} onClick={fetch} loading={loading} type='tertiary' size='small'>{t('刷新')}</Button>
          </Space>
        }
      >
        <Table columns={columns} dataSource={data} loading={loading} rowKey='id' pagination={false} size='small' />
      </Card>

      <Modal title={editing ? t('编辑能力策略') : t('新建能力策略')} visible={modalVisible} onOk={handleSave} onCancel={() => setModalVisible(false)} okText={t('保存')} cancelText={t('取消')}>
        <Form layout='vertical'>
          <Form.Slot label={t('能力匹配模式')}>
            <Input value={form.capability_pattern} onChange={(v) => setForm({ ...form, capability_pattern: v })} placeholder='bg.llm.*' />
          </Form.Slot>
          <Form.Slot label={t('作用域')}>
            <Select value={form.scope} onChange={(v) => setForm({ ...form, scope: v })} style={{ width: '100%' }}>
              {SCOPE_OPTIONS.map((s) => <Select.Option key={s} value={s}>{s}</Select.Option>)}
            </Select>
          </Form.Slot>
          <Form.Slot label={t('Scope ID（platform 填 0）')}>
            <InputNumber value={form.scope_id} onChange={(v) => setForm({ ...form, scope_id: v })} min={0} style={{ width: '100%' }} />
          </Form.Slot>
          <Form.Slot label={t('行为')}>
            <Select value={form.action} onChange={(v) => setForm({ ...form, action: v })} style={{ width: '100%' }}>
              {ACTION_OPTIONS.map((a) => <Select.Option key={a} value={a}>{a}</Select.Option>)}
            </Select>
          </Form.Slot>
          <Form.Slot label={t('优先级')}>
            <InputNumber value={form.priority} onChange={(v) => setForm({ ...form, priority: v })} min={0} style={{ width: '100%' }} />
          </Form.Slot>
          <Form.Slot label={t('描述')}>
            <Input value={form.description} onChange={(v) => setForm({ ...form, description: v })} placeholder={t('策略描述')} />
          </Form.Slot>
          <Form.Slot label={t('强制执行')}>
            <Switch checked={form.enforced} onChange={(v) => setForm({ ...form, enforced: v })} />
          </Form.Slot>
        </Form>
      </Modal>
    </>
  );
}

// ---------------------------------------------------------------------------
// Routing Policies Tab
// ---------------------------------------------------------------------------

function RoutingPolicies({ t }) {
  const [data, setData] = useState([]);
  const [loading, setLoading] = useState(false);
  const [modalVisible, setModalVisible] = useState(false);
  const [editing, setEditing] = useState(null);
  const [form, setForm] = useState({});

  const fetch = useCallback(async () => {
    setLoading(true);
    try {
      const res = await API.get('/api/bg/policies/routing?size=200');
      if (res.data?.success) setData(res.data.data?.items || []);
    } catch {
      showError(t('加载路由策略失败'));
    }
    setLoading(false);
  }, [t]);

  useEffect(() => { fetch(); }, [fetch]);

  const openCreate = () => {
    setEditing(null);
    setForm({ scope: 'platform', scope_id: 0, capability_pattern: '', strategy: 'weighted', rules_json: '', priority: 0, description: '' });
    setModalVisible(true);
  };

  const openEdit = (row) => {
    setEditing(row);
    setForm({ ...row });
    setModalVisible(true);
  };

  const handleSave = async () => {
    if (!form.capability_pattern?.trim()) {
      Toast.warning(t('请输入能力匹配模式'));
      return;
    }
    try {
      const payload = { ...form };
      let res;
      if (editing) {
        res = await API.put(`/api/bg/policies/routing/${editing.id}`, payload);
      } else {
        res = await API.post('/api/bg/policies/routing', payload);
      }
      if (res.data?.success) {
        Toast.success(editing ? t('更新成功') : t('创建成功'));
        setModalVisible(false);
        fetch();
      } else {
        Toast.error(res.data?.message || t('操作失败'));
      }
    } catch {
      Toast.error(t('操作失败'));
    }
  };

  const handleDelete = async (id) => {
    try {
      const res = await API.delete(`/api/bg/policies/routing/${id}`);
      if (res.data?.success) {
        Toast.success(t('删除成功'));
        fetch();
      } else {
        Toast.error(res.data?.message || t('删除失败'));
      }
    } catch {
      Toast.error(t('删除失败'));
    }
  };

  const columns = [
    { title: 'ID', dataIndex: 'id', width: 60 },
    {
      title: t('能力匹配'),
      dataIndex: 'capability_pattern',
      width: 220,
      render: (v) => <Text code style={{ fontSize: 12 }}>{v}</Text>,
    },
    {
      title: t('作用域'),
      dataIndex: 'scope',
      width: 100,
      render: (v) => <Tag color={SCOPE_COLORS[v] || 'grey'}>{v}</Tag>,
    },
    { title: t('Scope ID'), dataIndex: 'scope_id', width: 80 },
    {
      title: t('策略'),
      dataIndex: 'strategy',
      width: 120,
      render: (v) => <Tag>{v}</Tag>,
    },
    { title: t('优先级'), dataIndex: 'priority', width: 80 },
    { title: t('描述'), dataIndex: 'description' },
    {
      title: t('操作'),
      width: 140,
      render: (_, row) => (
        <Space>
          <Button icon={<IconEdit />} size='small' onClick={() => openEdit(row)} />
          <Popconfirm title={t('确定删除？')} onConfirm={() => handleDelete(row.id)}>
            <Button icon={<IconDelete />} size='small' type='danger' />
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <>
      <Card
        style={{ marginTop: 12 }}
        headerExtraContent={
          <Space>
            <Button icon={<IconPlus />} theme='solid' size='small' onClick={openCreate}>{t('新建')}</Button>
            <Button icon={<IconRefresh />} onClick={fetch} loading={loading} type='tertiary' size='small'>{t('刷新')}</Button>
          </Space>
        }
      >
        <Table columns={columns} dataSource={data} loading={loading} rowKey='id' pagination={false} size='small' />
      </Card>

      <Modal title={editing ? t('编辑路由策略') : t('新建路由策略')} visible={modalVisible} onOk={handleSave} onCancel={() => setModalVisible(false)} okText={t('保存')} cancelText={t('取消')}>
        <Form layout='vertical'>
          <Form.Slot label={t('能力匹配模式')}>
            <Input value={form.capability_pattern} onChange={(v) => setForm({ ...form, capability_pattern: v })} placeholder='bg.llm.chat.*' />
          </Form.Slot>
          <Form.Slot label={t('作用域')}>
            <Select value={form.scope} onChange={(v) => setForm({ ...form, scope: v })} style={{ width: '100%' }}>
              {SCOPE_OPTIONS.map((s) => <Select.Option key={s} value={s}>{s}</Select.Option>)}
            </Select>
          </Form.Slot>
          <Form.Slot label={t('Scope ID（platform 填 0）')}>
            <InputNumber value={form.scope_id} onChange={(v) => setForm({ ...form, scope_id: v })} min={0} style={{ width: '100%' }} />
          </Form.Slot>
          <Form.Slot label={t('路由策略')}>
            <Select value={form.strategy} onChange={(v) => setForm({ ...form, strategy: v })} style={{ width: '100%' }}>
              {STRATEGY_OPTIONS.map((s) => <Select.Option key={s} value={s}>{s}</Select.Option>)}
            </Select>
          </Form.Slot>
          <Form.Slot label={t('规则 JSON')}>
            <Input value={form.rules_json} onChange={(v) => setForm({ ...form, rules_json: v })} placeholder='{"adapters":[...]}' />
          </Form.Slot>
          <Form.Slot label={t('优先级')}>
            <InputNumber value={form.priority} onChange={(v) => setForm({ ...form, priority: v })} min={0} style={{ width: '100%' }} />
          </Form.Slot>
          <Form.Slot label={t('描述')}>
            <Input value={form.description} onChange={(v) => setForm({ ...form, description: v })} placeholder={t('策略描述')} />
          </Form.Slot>
        </Form>
      </Modal>
    </>
  );
}

// ---------------------------------------------------------------------------
// Main Page
// ---------------------------------------------------------------------------

export default function BgPolicies() {
  const { t } = useTranslation();

  return (
    <div style={{ padding: '24px', maxWidth: 1400, margin: '60px auto 0' }}>
      <div style={{ marginBottom: 16 }}>
        <Title heading={4} style={{ margin: 0 }}>{t('BaseGate 策略管理')}</Title>
        <Text type='tertiary' size='small'>{t('配置能力访问控制和请求路由策略')}</Text>
      </div>

      <Tabs type='line'>
        <TabPane tab={t('能力策略')} itemKey='cap'>
          <CapabilityPolicies t={t} />
        </TabPane>
        <TabPane tab={t('路由策略')} itemKey='route'>
          <RoutingPolicies t={t} />
        </TabPane>
      </Tabs>
    </div>
  );
}
