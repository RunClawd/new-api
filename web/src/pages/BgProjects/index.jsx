import React, { useState, useCallback } from 'react';
import { Card, Table, Tag, Button, Space, Input, Modal, Typography, Form, Popconfirm } from '@douyinfe/semi-ui';
import { IconSearch, IconRefresh, IconPlus, IconEdit, IconDelete } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, showError, timestamp2string } from '../../helpers';

const { Title, Text } = Typography;

export default function BgProjectsPage() {
  const { t } = useTranslation();
  const [rows, setRows] = useState([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [orgId, setOrgId] = useState('');
  
  const [modalVisible, setModalVisible] = useState(false);
  const [modalSubmitting, setModalSubmitting] = useState(false);
  const [editingRecord, setEditingRecord] = useState(null);
  
  const formApi = React.useRef(null);

  const fetchData = useCallback(async () => {
    setLoading(true);
    try {
      const params = new URLSearchParams({ p: page, page_size: pageSize });
      if (orgId) params.set('org_id', orgId);
      const res = await API.get(`/api/bg/projects?${params}`);
      if (res.data?.success) {
        const d = res.data.data;
        setRows(d.items ?? []);
        setTotal(d.total ?? 0);
      } else {
        showError(res.data?.message || t('获取失败'));
      }
    } catch (e) {
      showError(t('获取项目列表失败'));
    } finally {
      setLoading(false);
    }
  }, [page, pageSize, orgId]);

  React.useEffect(() => { fetchData(); }, [fetchData]);

  const handleEdit = (record) => {
    setEditingRecord(record);
    setModalVisible(true);
  };

  const handleAdd = () => {
    setEditingRecord(null);
    setModalVisible(true);
  };

  const handleDelete = async (record) => {
    try {
      const res = await API.delete(`/api/bg/projects/${record.project_id}`);
      if (res.data?.success) {
        fetchData();
      } else {
        showError(res.data?.message || t('删除失败'));
      }
    } catch (e) {
      showError(t('删除失败'));
    }
  };

  const handleSubmit = async (values) => {
    setModalSubmitting(true);
    try {
      if (editingRecord) {
        const res = await API.put(`/api/bg/projects/${editingRecord.project_id}`, {
          name: values.name,
          description: values.description,
          status: values.status,
        });
        if (res.data?.success) {
          setModalVisible(false);
          fetchData();
        } else {
          showError(res.data?.message || t('更新失败'));
        }
      } else {
        const res = await API.post(`/api/bg/projects`, {
          name: values.name,
          description: values.description,
          org_id: values.org_id ? Number(values.org_id) : 0,
        });
        if (res.data?.success) {
          setModalVisible(false);
          fetchData();
        } else {
          showError(res.data?.message || t('创建失败'));
        }
      }
    } catch (e) {
      showError(t('提交失败'));
    } finally {
      setModalSubmitting(false);
    }
  };

  const columns = [
    { title: 'Project ID', dataIndex: 'project_id', width: 220, render: (v) => <Text code>{v}</Text> },
    { title: t('组织 ID'), dataIndex: 'org_id', width: 100 },
    { title: t('名称'), dataIndex: 'name', width: 150, render: v => <Text strong>{v}</Text> },
    { title: t('描述'), dataIndex: 'description', ellipsis: true },
    { title: t('状态'), dataIndex: 'status', width: 100, render: (v) => <Tag color={v==='active'?'green':'grey'}>{v==='active'?'活跃':'已归档'}</Tag> },
    { title: t('创建时间'), dataIndex: 'created_at', width: 150, render: timestamp2string },
    {
      title: t('操作'),
      width: 120,
      render: (_, r) => (
        <Space>
          <Button size='small' type='tertiary' icon={<IconEdit />} onClick={() => handleEdit(r)} />
          <Popconfirm title={t('确定删除该项目？')} onConfirm={() => handleDelete(r)}>
            <Button size='small' type='danger' icon={<IconDelete />} />
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <div style={{ padding: '24px', maxWidth: 1400, margin: '64px auto 0' }}>
      <div style={{ marginBottom: 20 }}>
        <Title heading={4} style={{ margin: 0 }}>{t('BaseGate 项目管理')}</Title>
        <Text type='tertiary' size='small'>{t('配置租户项目的名称与描述')}</Text>
      </div>

      <Card shadows='hover' style={{ borderRadius: 12 }} bodyStyle={{ padding: 0 }}>
        <div style={{ padding: '16px 20px 8px', display: 'flex', justifyContent: 'space-between' }}>
          <Space wrap>
            <Input
              value={orgId}
              onChange={setOrgId}
              placeholder={t('按 Org ID 筛选')}
              style={{ width: 160 }}
              prefix={<IconSearch size='small' />}
              showClear
            />
            <Button icon={<IconRefresh />} onClick={fetchData} loading={loading} type='tertiary'>
              {t('刷新')}
            </Button>
          </Space>
          <Button icon={<IconPlus />} theme="solid" type="primary" onClick={handleAdd}>
            {t('新建项目')}
          </Button>
        </div>
        <Table
          columns={columns}
          dataSource={rows}
          loading={loading}
          rowKey='project_id'
          pagination={{
            currentPage: page,
            pageSize,
            total,
            onPageChange: setPage,
            onPageSizeChange: (v) => {
              setPageSize(v);
              setPage(1);
            },
            pageSizeOpts: [10, 20, 50],
            showSizeChanger: true,
            showTotal: true,
          }}
          scroll={{ x: 'max-content' }}
          size='middle'
        />
      </Card>

      <Modal
        title={editingRecord ? t('编辑项目') : t('新建项目')}
        visible={modalVisible}
        onCancel={() => setModalVisible(false)}
        footer={null}
        width={400}
      >
        <Form
          getFormApi={(api) => { formApi.current = api; }}
          onSubmit={handleSubmit}
          initValues={editingRecord ? {
            name: editingRecord.name,
            description: editingRecord.description,
            status: editingRecord.status,
          } : {
            org_id: 0,
            status: 'active'
          }}
        >
          {({ formState, values, formApi }) => (
            <>
              <Form.Input field="name" label={t('项目名称')} rules={[{ required: true, message: t('此项必填') }]} />
              <Form.TextArea field="description" label={t('描述')} />
              {!editingRecord && (
                <Form.InputNumber field="org_id" label={t('所有权 (Org ID)')} hideButtons initValue={0} />
              )}
              {editingRecord && (
                <Form.Select field="status" label={t('状态')} style={{ width: '100%' }}>
                  <Form.Select.Option value="active">活跃</Form.Select.Option>
                  <Form.Select.Option value="archived">已归档</Form.Select.Option>
                </Form.Select>
              )}
              <div style={{ display: 'flex', justifyContent: 'flex-end', marginTop: 24, gap: 8 }}>
                <Button onClick={() => setModalVisible(false)}>{t('取消')}</Button>
                <Button type="primary" theme="solid" htmlType="submit" loading={modalSubmitting}>{t('保存')}</Button>
              </div>
            </>
          )}
        </Form>
      </Modal>
    </div>
  );
}
