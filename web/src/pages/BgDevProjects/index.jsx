import React, { useState, useEffect, useCallback } from 'react';
import {
  Card,
  Table,
  Button,
  Modal,
  Form,
  Input,
  Typography,
  Toast,
  Tag,
  Popconfirm,
} from '@douyinfe/semi-ui';
import { IconPlus, IconDelete } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, showError } from '../../helpers';

const { Title, Text } = Typography;

export default function BgDevProjects() {
  const { t } = useTranslation();
  const [projects, setProjects] = useState([]);
  const [loading, setLoading] = useState(false);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize] = useState(20);
  const [createVisible, setCreateVisible] = useState(false);
  const [newName, setNewName] = useState('');
  const [newDesc, setNewDesc] = useState('');

  const fetchProjects = useCallback(async () => {
    setLoading(true);
    try {
      const res = await API.get(`/api/bg/dev/projects?p=${page - 1}&size=${pageSize}`);
      if (res.data?.success) {
        const data = res.data.data;
        setProjects(data.items || []);
        setTotal(data.total || 0);
      } else {
        showError(res.data?.message || t('获取失败'));
      }
    } catch {
      showError(t('加载项目失败'));
    }
    setLoading(false);
  }, [page, pageSize, t]);

  useEffect(() => {
    fetchProjects();
  }, [fetchProjects]);

  const handleCreate = async () => {
    if (!newName.trim()) {
      Toast.warning(t('请输入项目名称'));
      return;
    }
    try {
      const res = await API.post('/api/bg/dev/projects', {
        name: newName,
        description: newDesc,
      });
      if (res.data?.success) {
        Toast.success(t('创建成功'));
        setCreateVisible(false);
        setNewName('');
        setNewDesc('');
        fetchProjects();
      } else {
        Toast.error(res.data?.message || t('创建失败'));
      }
    } catch {
      Toast.error(t('创建失败'));
    }
  };

  const statusColors = { active: 'green', archived: 'grey' };

  const columns = [
    {
      title: t('项目 ID'),
      dataIndex: 'project_id',
      width: 200,
      render: (v) => <Text copyable={{ content: v }} code style={{ fontSize: 12 }}>{v}</Text>,
    },
    { title: t('名称'), dataIndex: 'name', width: 200 },
    { title: t('描述'), dataIndex: 'description' },
    {
      title: t('状态'),
      dataIndex: 'status',
      width: 80,
      render: (v) => (
        <Tag color={statusColors[v] || 'grey'}>
          {v === 'active' ? t('活跃') : t('已归档')}
        </Tag>
      ),
    },
    {
      title: t('创建时间'),
      dataIndex: 'created_at',
      width: 180,
      render: (v) => (v ? new Date(v * 1000).toLocaleString() : '-'),
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
          <Title heading={4} style={{ margin: 0 }}>{t('我的项目')}</Title>
          <Text type='tertiary' size='small'>
            {t('管理您的 BaseGate 项目，项目用于组织 API 密钥和隔离用量统计')}
          </Text>
        </div>
        <Button
          icon={<IconPlus />}
          theme='solid'
          onClick={() => setCreateVisible(true)}
        >
          {t('创建项目')}
        </Button>
      </div>

      <Card shadows='hover' style={{ borderRadius: 12 }}>
        <Table
          columns={columns}
          dataSource={projects}
          loading={loading}
          pagination={{
            currentPage: page,
            pageSize,
            total,
            onPageChange: setPage,
          }}
          rowKey='project_id'
          size='middle'
          empty={
            <div style={{ padding: '32px 0', textAlign: 'center', color: 'var(--semi-color-text-2)' }}>
              {t('暂无项目，点击「创建项目」开始使用')}
            </div>
          }
        />
      </Card>

      <Modal
        title={t('创建项目')}
        visible={createVisible}
        onOk={handleCreate}
        onCancel={() => setCreateVisible(false)}
        okText={t('创建')}
        cancelText={t('取消')}
      >
        <Form layout='vertical'>
          <Form.Slot label={t('项目名称')}>
            <Input
              value={newName}
              onChange={setNewName}
              placeholder={t('输入项目名称')}
            />
          </Form.Slot>
          <Form.Slot label={t('描述（可选）')}>
            <Input
              value={newDesc}
              onChange={setNewDesc}
              placeholder={t('输入项目描述')}
            />
          </Form.Slot>
        </Form>
      </Modal>
    </div>
  );
}
