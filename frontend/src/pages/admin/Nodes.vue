<template>
  <div class="admin-nodes">
    <div class="header">
      <h2>出口节点管理</h2>
      <div>
        <el-button type="success" @click="showDeployDialog">一键部署</el-button>
        <el-button type="primary" @click="showAddDialog">新增节点</el-button>
      </div>
    </div>

    <div v-if="selectedServers.length" class="batch-toolbar">
      <span>已选择 {{ selectedServers.length }} 台节点服务器</span>
      <div>
        <el-button size="small" @click="clearServerSelection">取消选择</el-button>
        <el-button size="small" type="danger" :loading="batchDeleting" @click="handleBatchDeleteServers">批量删除</el-button>
      </div>
    </div>

    <el-table
      ref="serversTableRef"
      :data="nodeServers"
      border
      style="width: 100%"
      v-loading="loading"
      row-key="key"
      @selection-change="handleServerSelectionChange"
    >
      <el-table-column type="selection" width="48" />
      <el-table-column type="expand" width="44">
        <template #default="{ row }">
          <el-table :data="row.nodes" border size="small" class="line-table">
            <el-table-column prop="id" label="线路 ID" width="80" />
            <el-table-column prop="name" label="线路名称" min-width="170" />
            <el-table-column label="入口" min-width="150">
              <template #default="{ row: line }">{{ line.host }}:{{ line.port }}</template>
            </el-table-column>
            <el-table-column label="协议" width="100">
              <template #default="{ row: line }">
                <el-tag effect="plain" :type="line.transport === 'xhttp' ? 'warning' : 'info'">{{ transportLabel(line.transport) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="流量池" width="90">
              <template #default="{ row: line }">
                <el-tag effect="plain" :type="line.traffic_pool === 'residential' ? 'warning' : 'success'">
                  {{ line.traffic_pool === 'residential' ? '家宽' : '普通' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="出站" min-width="260">
              <template #default="{ row: line }">
                <div class="line-outbound">
                  <span>{{ line.outbound_type === 'socks5' ? 'SOCKS5' : '本机直连' }}</span>
                  <small v-if="line.outbound_ip">本机源 IP {{ line.outbound_ip }}</small>
                  <small v-if="line.outbound_proxy_url">{{ maskProxyUrl(line.outbound_proxy_url) }}</small>
                </div>
              </template>
            </el-table-column>
            <el-table-column label="线路输出" width="120">
              <template #default="{ row: line }">
                <el-tag effect="plain">{{ lineModeLabel(line.line_mode) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="状态" width="80">
              <template #default="{ row: line }">
                <el-tag :type="line.is_enabled ? 'success' : 'info'">{{ line.is_enabled ? '启用' : '禁用' }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="操作" width="150">
              <template #default="{ row: line }">
                <el-button size="small" @click="showEditDialog(line)">编辑</el-button>
                <el-button size="small" type="danger" @click="handleDelete(line)">删除</el-button>
              </template>
            </el-table-column>
          </el-table>
        </template>
      </el-table-column>
      <el-table-column label="节点服务器" min-width="220">
        <template #default="{ row }">
          <div class="server-cell">
            <strong>{{ row.name }}</strong>
            <small>{{ row.node_host_id ? `Host #${row.node_host_id}` : `单线路 #${row.nodes[0]?.id}` }}</small>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="IP 信息" min-width="300">
        <template #default="{ row }">
          <div class="ip-cell">
            <div>管理 IP：{{ row.management_ip || '-' }}</div>
            <div class="ip-tags">
              <el-tag v-for="ip in row.ips" :key="ip" size="small" effect="plain">{{ ip }}</el-tag>
            </div>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="能力摘要" min-width="300">
        <template #default="{ row }">
          <div class="capability-tags">
            <el-tag size="small" type="success" effect="plain">普通 {{ row.normal_count }}</el-tag>
            <el-tag size="small" type="warning" effect="plain">家宽 {{ row.residential_count }}</el-tag>
            <el-tag size="small" effect="plain">SOCKS5 {{ row.socks5_count }}</el-tag>
            <el-tag size="small" effect="plain">TCP {{ row.tcp_count }}</el-tag>
            <el-tag size="small" type="warning" effect="plain">XHTTP {{ row.xhttp_count }}</el-tag>
            <el-tag size="small" type="info" effect="plain">线路 {{ row.nodes.length }}</el-tag>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="状态" width="110">
        <template #default="{ row }">
          <el-tag :type="row.enabled_count ? 'success' : 'info'">{{ row.enabled_count ? '启用' : '禁用' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="最后心跳" width="180">
        <template #default="{ row }">{{ row.last_heartbeat_at ? formatDate(row.last_heartbeat_at) : '-' }}</template>
      </el-table-column>
      <el-table-column label="流量上报" min-width="220">
        <template #default="{ row }">
          <div class="traffic-sync-cell">
            <div>
              <el-tag size="small" :type="trafficSyncTag(row)">{{ trafficSyncLabel(row) }}</el-tag>
              <span class="traffic-sync-time">{{ row.last_traffic_success_at ? formatDate(row.last_traffic_success_at) : '未成功' }}</span>
            </div>
            <small v-if="row.last_traffic_error" class="traffic-sync-error">{{ row.last_traffic_error }}</small>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="340">
        <template #default="{ row }">
          <el-button size="small" type="primary" @click="showServerDialog(row)">编辑服务器</el-button>
          <el-button size="small" @click="showAddResidentialForServer(row)">加家宽</el-button>
          <el-button size="small" type="warning" @click="showRepairCenterDialog(row)">修复中心</el-button>
          <el-button size="small" type="danger" @click="handleDeleteServer(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="dialogVisible" :title="isEdit ? '编辑节点' : '新增节点'" width="600px">
      <el-form :model="form" :rules="rules" ref="formRef" label-width="120px">
        <el-form-item label="节点名称" prop="name">
          <el-input v-model="form.name" />
        </el-form-item>
        <el-form-item label="地址" prop="host">
          <el-input v-model="form.host" placeholder="例如：hk.example.com" />
        </el-form-item>
        <el-form-item label="流量池">
          <el-select v-model="form.traffic_pool" style="width: 100%">
            <el-option label="普通流量" value="normal" />
            <el-option label="家宽流量" value="residential" />
          </el-select>
        </el-form-item>
        <el-form-item label="出站方式">
          <el-select v-model="form.outbound_type" style="width: 100%">
            <el-option label="本机直连" value="direct" />
            <el-option label="上游 SOCKS5" value="socks5" />
          </el-select>
        </el-form-item>
        <el-form-item v-if="form.outbound_type === 'socks5'" label="上游代理 URL">
          <el-input
            v-model="form.outbound_proxy_url"
            type="textarea"
            :rows="4"
            placeholder="每行一个 socks5://user:pass@host:port"
          />
        </el-form-item>
        <el-form-item v-if="form.outbound_type === 'socks5'" label="本机源 IP">
          <el-select
            v-model="form.outbound_ip"
            filterable
            allow-create
            clearable
            placeholder="自动选择，或指定连接上游 SOCKS5 的本机出口 IP"
            style="width: 100%"
          >
            <el-option v-for="ip in outboundIpOptions" :key="ip" :label="ip" :value="ip" />
          </el-select>
        </el-form-item>
        <el-form-item label="传输模式">
          <el-select v-model="form.transports" multiple :multiple-limit="isEdit ? 1 : 2" style="width: 100%" @change="handleTransportSelectionChange(form)">
            <el-option label="TCP + Reality" value="tcp" />
            <el-option label="XHTTP + Reality" value="xhttp" />
          </el-select>
        </el-form-item>
        <el-form-item v-if="hasTransport(form, 'tcp')" label="TCP 端口" prop="tcp_port">
          <el-input-number v-model="form.tcp_port" :min="1" :max="65535" />
        </el-form-item>
        <template v-if="hasTransport(form, 'xhttp')">
          <el-form-item label="XHTTP 端口" prop="xhttp_port">
            <el-input-number v-model="form.xhttp_port" :min="1" :max="65535" />
          </el-form-item>
          <el-form-item label="XHTTP Path">
            <el-input v-model="form.xhttp_path" placeholder="/raypilot" />
          </el-form-item>
          <el-form-item label="XHTTP Mode">
            <el-select v-model="form.xhttp_mode" style="width: 100%">
              <el-option label="auto" value="auto" />
              <el-option label="packet-up" value="packet-up" />
              <el-option label="stream-up" value="stream-up" />
              <el-option label="stream-one" value="stream-one" />
            </el-select>
          </el-form-item>
          <el-form-item label="XHTTP Host">
            <el-input v-model="form.xhttp_host" placeholder="可选" />
          </el-form-item>
        </template>
        <el-form-item label="Server Name">
          <el-input v-model="form.server_name" placeholder="Reality SNI" />
        </el-form-item>
        <el-form-item label="Public Key">
          <el-input v-model="form.public_key" placeholder="Reality 公钥" />
        </el-form-item>
        <el-form-item label="Short ID">
          <el-input v-model="form.short_id" placeholder="Reality Short ID" />
        </el-form-item>
        <el-form-item label="线路输出">
          <el-select v-model="form.line_mode" style="width: 100%">
            <el-option label="直连 + 中转" value="direct_and_relay" />
            <el-option label="仅直连" value="direct_only" />
            <el-option label="仅中转" value="relay_only" />
          </el-select>
        </el-form-item>
        <el-form-item label="Agent 地址" prop="agent_base_url">
          <el-input v-model="form.agent_base_url" placeholder="http://node-ip:port" />
        </el-form-item>
        <el-form-item label="Agent Token" prop="agent_token">
          <el-input v-model="form.agent_token" type="password" show-password placeholder="节点鉴权 Token" />
        </el-form-item>
        <el-form-item label="状态">
          <el-switch v-model="form.is_enabled" active-text="启用" inactive-text="禁用" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleSave" :loading="saving">保存</el-button>
      </template>
    </el-dialog>

    <!-- 一键部署弹窗 -->
    <el-dialog v-model="deployDialogVisible" title="一键部署节点" width="760px">
      <el-form :model="deployForm" :rules="deployRules" ref="deployFormRef" label-width="120px">
        <el-alert :title="deployForm.multi_ip_enabled ? '多 IP 模式会先扫描出口 IP，勾选确认后才会创建多个逻辑节点' : '部署成功后会自动创建节点记录并刷新列表'" type="info" :closable="false" style="margin-bottom: 16px" />
        <el-form-item label="服务器 IP" prop="ssh_host">
          <el-input v-model="deployForm.ssh_host" placeholder="例如：154.219.97.219" />
        </el-form-item>
        <el-form-item label="SSH 端口" prop="ssh_port">
          <el-input-number v-model="deployForm.ssh_port" :min="1" :max="65535" />
        </el-form-item>
        <el-form-item label="SSH 用户" prop="ssh_user">
          <el-input v-model="deployForm.ssh_user" placeholder="例如：root" />
        </el-form-item>
        <el-form-item label="SSH 密码" prop="ssh_password">
          <el-input v-model="deployForm.ssh_password" type="password" show-password />
        </el-form-item>
        <el-form-item label="节点名称" prop="node_name">
          <el-input v-model="deployForm.node_name" placeholder="可选，默认为 raypilot-node-IP" />
        </el-form-item>
        <el-form-item label="中心服务地址" prop="center_url">
          <el-input v-model="deployForm.center_url" placeholder="例如：http://leiyunai.fun" />
        </el-form-item>
        <el-form-item label="备用中心地址">
          <el-input
            v-model="deployForm.center_urls_text"
            type="textarea"
            :rows="3"
            placeholder="每行一个备用 IP 或域名，也可以用逗号分隔"
          />
        </el-form-item>
        <el-form-item label="节点 Token">
          <el-input v-model="deployForm.node_token" placeholder="留空自动生成" />
        </el-form-item>
        <el-form-item label="流量池">
          <el-select v-model="deployForm.traffic_pool" style="width: 100%">
            <el-option label="普通流量" value="normal" />
            <el-option label="家宽流量" value="residential" />
          </el-select>
        </el-form-item>
        <el-form-item label="出站方式">
          <el-select v-model="deployForm.outbound_type" style="width: 100%">
            <el-option label="本机直连" value="direct" />
            <el-option label="上游 SOCKS5" value="socks5" />
          </el-select>
        </el-form-item>
        <el-form-item v-if="deployForm.outbound_type === 'socks5'" label="上游代理 URL">
          <el-input
            v-model="deployForm.outbound_proxy_url"
            type="textarea"
            :rows="6"
            placeholder="每行一个 socks5://user:pass@host:port"
          />
        </el-form-item>
        <el-form-item v-if="deployForm.outbound_type === 'socks5'" label="本机源 IP">
          <el-select
            v-model="deployForm.outbound_ip"
            filterable
            allow-create
            clearable
            placeholder="自动选择，或指定连接上游 SOCKS5 的本机出口 IP"
            style="width: 100%"
          >
            <el-option v-for="ip in deployOutboundIpOptions" :key="ip" :label="ip" :value="ip" />
          </el-select>
        </el-form-item>
        <el-form-item label="节点分组">
          <el-select v-model="deployForm.node_group_ids" multiple filterable placeholder="部署成功后自动加入分组" style="width: 100%">
            <el-option v-for="group in nodeGroups" :key="group.id" :label="group.name" :value="group.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="替换旧角色">
          <el-switch
            v-model="deployForm.replace_existing_role"
            active-text="自动停用同服务器旧出口/中转"
            inactive-text="保留旧记录"
          />
        </el-form-item>
        <el-form-item label="传输模式">
          <el-select v-model="deployForm.transports" multiple :multiple-limit="2" style="width: 100%" @change="handleTransportSelectionChange(deployForm)">
            <el-option label="TCP + Reality" value="tcp" />
            <el-option label="XHTTP + Reality" value="xhttp" />
          </el-select>
        </el-form-item>
        <el-form-item v-if="hasTransport(deployForm, 'tcp')" label="TCP 端口">
          <el-input-number v-model="deployForm.tcp_port" :min="1" :max="65535" />
        </el-form-item>
        <template v-if="hasTransport(deployForm, 'xhttp')">
          <el-form-item label="XHTTP 端口">
            <el-input-number v-model="deployForm.xhttp_port" :min="1" :max="65535" />
          </el-form-item>
          <el-form-item label="XHTTP Path">
            <el-input v-model="deployForm.xhttp_path" placeholder="/raypilot" />
          </el-form-item>
          <el-form-item label="XHTTP Mode">
            <el-select v-model="deployForm.xhttp_mode" style="width: 100%">
              <el-option label="auto" value="auto" />
              <el-option label="packet-up" value="packet-up" />
              <el-option label="stream-up" value="stream-up" />
              <el-option label="stream-one" value="stream-one" />
            </el-select>
          </el-form-item>
          <el-form-item label="XHTTP Host">
            <el-input v-model="deployForm.xhttp_host" placeholder="可选" />
          </el-form-item>
        </template>
        <el-form-item label="多 IP 服务器">
          <el-switch
            v-model="deployForm.multi_ip_enabled"
            active-text="是"
            inactive-text="否"
            @change="handleMultiIpModeChange"
          />
        </el-form-item>
        <template v-if="deployForm.multi_ip_enabled">
          <el-form-item label="出口 IP 扫描">
            <el-button type="primary" plain :loading="scanningIps" @click="handleScanDeployIps">扫描出口 IP</el-button>
            <span class="scan-hint">扫描后手动勾选要创建为节点的公网出口 IP</span>
          </el-form-item>
          <el-table
            v-if="scannedIps.length"
            ref="scanIpsTableRef"
            :data="scannedIps"
            border
            size="small"
            class="scan-ip-table"
            @selection-change="handleScanIpSelectionChange"
          >
            <el-table-column type="selection" width="48" :selectable="isScannedIpSelectable" />
            <el-table-column prop="ip" label="IP" min-width="150" />
            <el-table-column prop="interface" label="网卡" width="110" />
            <el-table-column label="状态" width="120">
              <template #default="{ row }">
                <el-tag :type="row.is_usable ? 'success' : row.status === 'skipped' ? 'info' : 'danger'">
                  {{ row.is_usable ? '可用' : row.status === 'skipped' ? '跳过' : '不可用' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="message" label="说明" min-width="220" />
          </el-table>
        </template>
      </el-form>

      <!-- 部署进度 -->
      <div v-if="deploySteps.length > 0" class="deploy-steps">
        <div v-for="(step, index) in deploySteps" :key="index" class="deploy-step">
          <el-icon :class="step.status">
            <CircleCheck v-if="step.status === 'success'" />
            <CircleClose v-else-if="step.status === 'failed'" />
            <Loading v-else />
          </el-icon>
          <span>{{ step.name }}</span>
          <span class="step-msg">{{ step.message }}</span>
        </div>
      </div>

      <template #footer>
        <el-button @click="deployDialogVisible = false" :disabled="deploying">取消</el-button>
        <el-button type="success" @click="handleDeploy" :loading="deploying" :disabled="deploying">开始部署</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="serverDialogVisible" title="编辑节点服务器" width="920px">
      <div v-if="activeServer" class="server-dialog">
        <div class="server-dialog-summary">
          <div>
            <strong>{{ activeServer.name }}</strong>
            <small>管理 IP：{{ activeServer.management_ip || '-' }}</small>
          </div>
          <div class="ip-tags">
            <el-tag v-for="ip in activeServer.ips" :key="ip" size="small" effect="plain">{{ ip }}</el-tag>
          </div>
          <div class="capability-tags">
            <el-tag size="small" type="success" effect="plain">普通 {{ activeServer.normal_count }}</el-tag>
            <el-tag size="small" type="warning" effect="plain">家宽 {{ activeServer.residential_count }}</el-tag>
            <el-tag size="small" effect="plain">线路 {{ activeServer.nodes.length }}</el-tag>
          </div>
        </div>
        <div class="server-dialog-actions">
          <el-button type="primary" @click="showAddNormalForServer(activeServer)">加普通线路</el-button>
          <el-button @click="showAddResidentialForServer(activeServer)">加家宽线路</el-button>
        </div>
        <el-table :data="activeServer.nodes" border size="small">
          <el-table-column prop="id" label="线路 ID" width="80" />
          <el-table-column prop="name" label="线路名称" min-width="170" />
          <el-table-column label="入口" min-width="150">
            <template #default="{ row }">{{ row.host }}:{{ row.port }}</template>
          </el-table-column>
          <el-table-column label="协议" width="100">
            <template #default="{ row }">{{ transportLabel(row.transport) }}</template>
          </el-table-column>
          <el-table-column label="流量池" width="90">
            <template #default="{ row }">{{ row.traffic_pool === 'residential' ? '家宽' : '普通' }}</template>
          </el-table-column>
          <el-table-column label="出站" min-width="260">
            <template #default="{ row }">
              <div class="line-outbound">
                <span>{{ row.outbound_type === 'socks5' ? 'SOCKS5' : '本机直连' }}</span>
                <small v-if="row.outbound_ip">本机源 IP {{ row.outbound_ip }}</small>
                <small v-if="row.outbound_proxy_url">{{ maskProxyUrl(row.outbound_proxy_url) }}</small>
              </div>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="150">
            <template #default="{ row }">
              <el-button size="small" @click="showEditDialog(row)">编辑</el-button>
              <el-button size="small" type="danger" @click="handleDelete(row)">删除</el-button>
            </template>
          </el-table-column>
        </el-table>
      </div>
      <template #footer>
        <el-button @click="serverDialogVisible = false">关闭</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="repairDialogVisible" title="修复节点中心地址" width="680px">
      <el-form :model="repairForm" :rules="repairRules" ref="repairFormRef" label-width="130px">
        <el-alert
          title="兜底修复会通过 SSH 登录节点服务器，重建或重启 node-agent，并写入新的中心地址列表。"
          type="warning"
          :closable="false"
          style="margin-bottom: 16px"
        />
        <el-form-item label="服务器 IP" prop="ssh_host">
          <el-input v-model="repairForm.ssh_host" />
        </el-form-item>
        <el-form-item label="SSH 端口" prop="ssh_port">
          <el-input-number v-model="repairForm.ssh_port" :min="1" :max="65535" />
        </el-form-item>
        <el-form-item label="SSH 用户" prop="ssh_user">
          <el-input v-model="repairForm.ssh_user" />
        </el-form-item>
        <el-form-item label="SSH 密码" prop="ssh_password">
          <el-input v-model="repairForm.ssh_password" type="password" show-password />
        </el-form-item>
        <el-form-item label="主中心地址" prop="center_url">
          <el-input v-model="repairForm.center_url" placeholder="例如：http://leiyunai.fun" />
        </el-form-item>
        <el-form-item label="备用中心地址">
          <el-input
            v-model="repairForm.center_urls_text"
            type="textarea"
            :rows="4"
            placeholder="每行一个备用 IP 或域名，也可以用逗号分隔"
          />
        </el-form-item>
        <el-form-item label="等待心跳">
          <el-input-number v-model="repairForm.wait_timeout_seconds" :min="0" :max="300" />
          <span class="scan-hint">秒，填 0 表示只修配置不等待</span>
        </el-form-item>
      </el-form>
      <div v-if="repairSteps.length" class="deploy-steps">
        <div v-for="(step, index) in repairSteps" :key="index" class="deploy-step">
          <el-icon :class="step.status">
            <CircleCheck v-if="step.status === 'success'" />
            <CircleClose v-else-if="step.status === 'failed'" />
            <Loading v-else />
          </el-icon>
          <span>{{ step.name }}</span>
          <span class="step-msg">{{ step.message }}</span>
        </div>
      </div>
      <template #footer>
        <el-button @click="repairDialogVisible = false" :disabled="repairingCenter">取消</el-button>
        <el-button type="warning" @click="handleRepairCenter" :loading="repairingCenter">开始修复</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, ref, reactive, onMounted } from 'vue'
import { adminApi } from '@/api'
import { ElMessage, ElMessageBox } from 'element-plus'
import { CircleCheck, CircleClose, Loading } from '@element-plus/icons-vue'

const nodes = ref([])
const nodeGroups = ref([])
const loading = ref(false)
const dialogVisible = ref(false)
const isEdit = ref(false)
const editingId = ref(null)
const saving = ref(false)
const formRef = ref(null)
const serversTableRef = ref(null)
const selectedServers = ref([])
const batchDeleting = ref(false)
const serverDialogVisible = ref(false)
const activeServerKey = ref('')
const repairDialogVisible = ref(false)
const repairFormRef = ref(null)
const repairingCenter = ref(false)
const repairSteps = ref([])

const form = reactive({
  name: '',
  host: '',
  traffic_pool: 'normal',
  outbound_type: 'direct',
  outbound_ip: '',
  outbound_proxy_url: '',
  transports: ['tcp'],
  tcp_port: 443,
  xhttp_port: 443,
  xhttp_path: '/raypilot',
  xhttp_host: '',
  xhttp_mode: 'auto',
  server_name: '',
  public_key: '',
  short_id: '',
  line_mode: 'direct_and_relay',
  node_host_id: null,
  agent_base_url: '',
  agent_token: '',
  is_enabled: true,
})

const rules = {
  name: [{ required: true, message: '请输入节点名称', trigger: 'blur' }],
  host: [{ required: true, message: '请输入节点地址', trigger: 'blur' }],
  tcp_port: [{ required: true, message: '请输入 TCP 端口', trigger: 'blur' }],
  xhttp_port: [{ required: true, message: '请输入 XHTTP 端口', trigger: 'blur' }],
  agent_base_url: [{ required: true, message: '请输入 Agent 地址', trigger: 'blur' }],
  agent_token: [{
    message: '请输入 Agent Token',
    trigger: 'blur',
    validator: (rule, value, callback) => {
      if (!isEdit.value && !form.node_host_id && !value) {
        callback(new Error('请输入 Agent Token'))
      } else {
        callback()
      }
    },
  }],
}

// 一键部署
const deployDialogVisible = ref(false)
const deploying = ref(false)
const deployFormRef = ref(null)
const deploySteps = ref([])
const scannedIps = ref([])
const selectedDeployIps = ref([])
const scanningIps = ref(false)
const scanIpsTableRef = ref(null)

const deployForm = reactive({
  ssh_host: '',
  ssh_port: 22,
  ssh_user: 'root',
  ssh_password: '',
  node_name: '',
  traffic_pool: 'normal',
  outbound_type: 'direct',
  outbound_ip: '',
  outbound_proxy_url: '',
  center_url: window.location.origin,
  center_urls_text: defaultBackupCenterURLsText(),
  node_token: '',
  transports: ['tcp'],
  tcp_port: 443,
  xhttp_port: 443,
  xhttp_path: '/raypilot',
  xhttp_host: '',
  xhttp_mode: 'auto',
  multi_ip_enabled: false,
  node_group_ids: [],
  replace_existing_role: true,
})

const nodeServers = computed(() => aggregateNodeServers(nodes.value))
const activeServer = computed(() => nodeServers.value.find((server) => server.key === activeServerKey.value) || null)
const outboundIpOptions = computed(() => collectKnownIPs(activeServer.value?.nodes || nodes.value, form.host))
const deployOutboundIpOptions = computed(() => {
  const scanned = scannedIps.value.filter((item) => item.is_usable).map((item) => item.ip)
  return uniqueValues([deployForm.ssh_host, ...scanned])
})

const deployRules = {
  ssh_host: [{ required: true, message: '请输入服务器 IP', trigger: 'blur' }],
  ssh_user: [{ required: true, message: '请输入 SSH 用户', trigger: 'blur' }],
  ssh_password: [{ required: true, message: '请输入 SSH 密码', trigger: 'blur' }],
  center_url: [{ required: true, message: '请输入中心服务地址', trigger: 'blur' }],
}

const repairForm = reactive({
  ssh_host: '',
  ssh_port: 22,
  ssh_user: 'root',
  ssh_password: '',
  center_url: window.location.origin,
  center_urls_text: defaultBackupCenterURLsText(),
  node_id: 0,
  node_host_id: 0,
  wait_timeout_seconds: 45,
})

const repairRules = {
  ssh_host: [{ required: true, message: '请输入服务器 IP', trigger: 'blur' }],
  ssh_user: [{ required: true, message: '请输入 SSH 用户', trigger: 'blur' }],
  ssh_password: [{ required: true, message: '请输入 SSH 密码', trigger: 'blur' }],
  center_url: [{ required: true, message: '请输入主中心地址', trigger: 'blur' }],
}

function showDeployDialog() {
  deploySteps.value = []
  scannedIps.value = []
  selectedDeployIps.value = []
  deployForm.center_url = currentCenterURL()
  deployForm.center_urls_text = defaultBackupCenterURLsText()
  handleTransportSelectionChange(deployForm)
  deployDialogVisible.value = true
  fetchNodeGroups()
}

function handleMultiIpModeChange() {
  scannedIps.value = []
  selectedDeployIps.value = []
  scanIpsTableRef.value?.clearSelection()
}

function isScannedIpSelectable(row) {
  return !!row.is_usable
}

function handleScanIpSelectionChange(selection) {
  selectedDeployIps.value = selection
  if (deployForm.outbound_type === 'socks5' && !deployForm.outbound_ip && selection.length) {
    deployForm.outbound_ip = selection[0].ip
  }
}

async function handleScanDeployIps() {
  const valid = await deployFormRef.value.validate().catch(() => false)
  if (!valid) return

  scanningIps.value = true
  scannedIps.value = []
  selectedDeployIps.value = []
  try {
    const res = await adminApi.nodes.scanDeployIps({
      ssh_host: deployForm.ssh_host,
      ssh_port: deployForm.ssh_port,
      ssh_user: deployForm.ssh_user,
      ssh_password: deployForm.ssh_password,
    })
    scannedIps.value = res.data.ips || []
    deploySteps.value = res.data.steps || []
    const usableCount = scannedIps.value.filter((item) => item.is_usable).length
    if (usableCount) {
      ElMessage.success(`扫描完成，发现 ${usableCount} 个可用出口 IP`)
    } else {
      ElMessage.warning('扫描完成，未发现可用公网出口 IP')
    }
  } catch (err) {
    ElMessage.error(err.message || '出口 IP 扫描失败')
  } finally {
    scanningIps.value = false
  }
}

async function handleDeploy() {
  const valid = await deployFormRef.value.validate().catch(() => false)
  if (!valid) return

  deploying.value = true
  deploySteps.value = []

  try {
    if (deployForm.multi_ip_enabled && selectedDeployIps.value.length === 0) {
      ElMessage.warning('请先扫描并勾选要部署的出口 IP')
      return
    }
    if (deployForm.outbound_type === 'socks5' && !deployForm.outbound_proxy_url.trim()) {
      ElMessage.warning('请填写至少一条上游 SOCKS5 代理')
      return
    }
    const transports = normalizedTransports(deployForm)
    if (transports.includes('tcp') && transports.includes('xhttp') && deployForm.tcp_port === deployForm.xhttp_port) {
      ElMessage.warning('TCP 和 XHTTP 端口不能相同')
      return
    }
    const payload = {
      ssh_host: deployForm.ssh_host,
      ssh_port: deployForm.ssh_port,
      ssh_user: deployForm.ssh_user,
      ssh_password: deployForm.ssh_password,
      node_name: deployForm.node_name,
      traffic_pool: deployForm.traffic_pool,
      outbound_type: deployForm.outbound_type,
      outbound_ip: deployForm.outbound_ip,
      outbound_proxy_url: deployForm.outbound_proxy_url,
      center_url: deployForm.center_url,
      center_urls: parseCenterURLsText(deployForm.center_urls_text),
      node_token: deployForm.node_token,
      transports,
      transport: transports[0],
      tcp_port: deployForm.tcp_port,
      xhttp_port: deployForm.xhttp_port,
      xhttp_path: deployForm.xhttp_path,
      xhttp_host: deployForm.xhttp_host,
      xhttp_mode: deployForm.xhttp_mode,
      multi_ip_enabled: deployForm.multi_ip_enabled,
      selected_ips: selectedDeployIps.value.map((item) => item.ip),
      node_group_ids: deployForm.node_group_ids,
      replace_existing_role: deployForm.replace_existing_role,
    }

    const res = await adminApi.nodes.deploy(payload)
    deploySteps.value = res.data.steps || []

    if (res.data.success) {
      const ids = res.data.node_ids?.length ? res.data.node_ids.join(', ') : res.data.node_id
      ElMessage.success(`部署成功！节点 ID: ${ids}`)
      const generatedToken = res.data.node_host_token || res.data.node_token
      if (generatedToken) {
        await ElMessageBox.alert(`节点 Token 已自动生成：${generatedToken}`, '部署成功', {
          confirmButtonText: '知道了',
        })
      }
      deployDialogVisible.value = false
      await fetchNodes()
    } else {
      ElMessage.error('部署失败，请查看部署步骤详情')
    }
  } catch (err) {
    ElMessage.error(err.message || '部署失败')
  } finally {
    deploying.value = false
  }
}

function formatDate(dateStr) {
  if (!dateStr) return '-'
  return new Date(dateStr).toLocaleString('zh-CN')
}

function lineModeLabel(mode) {
  const labels = {
    direct_only: '仅直连',
    relay_only: '仅中转',
    direct_and_relay: '直连 + 中转',
  }
  return labels[mode] || '直连 + 中转'
}

function transportLabel(transport) {
  return transport === 'xhttp' ? 'XHTTP' : 'TCP'
}

function normalizedTransports(target) {
  const values = selectedTransports(target)
  return values.length ? values : ['tcp']
}

function selectedTransports(target) {
  const values = Array.isArray(target.transports) ? target.transports : []
  const set = new Set(values.filter((item) => item === 'tcp' || item === 'xhttp'))
  return ['tcp', 'xhttp'].filter((item) => set.has(item))
}

function hasTransport(target, transport) {
  return normalizedTransports(target).includes(transport)
}

function handleTransportSelectionChange(target) {
  const transports = selectedTransports(target)
  target.transports = transports
  if (!transports.length) return
  if (!target.tcp_port) target.tcp_port = 443
  if (!target.xhttp_port) target.xhttp_port = 443
  if (transports.includes('tcp') && transports.includes('xhttp') && target.tcp_port === target.xhttp_port) {
    target.xhttp_port = target.tcp_port === 443 ? 8443 : target.tcp_port + 1
  }
  if (transports.includes('xhttp')) {
    if (!target.xhttp_path) target.xhttp_path = '/raypilot'
    if (!target.xhttp_mode) target.xhttp_mode = 'auto'
  }
}

function trafficSyncTag(row) {
  if (row.last_traffic_error) return 'danger'
  if (row.last_traffic_success_at) return 'success'
  return 'info'
}

function trafficSyncLabel(row) {
  if (row.last_traffic_error) return `异常 ${row.traffic_error_count || 1} 次`
  if (row.last_traffic_success_at) return '正常'
  return '未上报'
}

function parseDateValue(value) {
  if (!value) return 0
  const timestamp = new Date(value).getTime()
  return Number.isNaN(timestamp) ? 0 : timestamp
}

function maxDateString(values) {
  return values.filter(Boolean).sort((a, b) => parseDateValue(b) - parseDateValue(a))[0] || ''
}

function uniqueValues(values) {
  const seen = new Set()
  const result = []
  for (const value of values) {
    const text = String(value || '').trim()
    if (!text || seen.has(text)) continue
    seen.add(text)
    result.push(text)
  }
  return result
}

function isIPv4(value) {
  const parts = String(value || '').trim().split('.')
  return parts.length === 4 && parts.every((part) => /^\d+$/.test(part) && Number(part) >= 0 && Number(part) <= 255)
}

function hostFromURL(value) {
  const text = String(value || '').trim()
  if (!text) return ''
  try {
    return new URL(text).hostname
  } catch {
    const match = text.match(/^[a-z]+:\/\/([^/:]+)/i)
    return match ? match[1] : ''
  }
}

function collectKnownIPs(lines, fallbackHost = '') {
  const values = [fallbackHost]
  for (const line of lines || []) {
    values.push(line.host, line.listen_ip, line.outbound_ip, hostFromURL(line.agent_base_url))
  }
  return uniqueValues(values.filter(isIPv4))
}

function managementIPForLines(lines) {
  for (const line of lines) {
    const agentHost = hostFromURL(line.agent_base_url)
    if (agentHost) return agentHost
  }
  for (const line of lines) {
    if (line.host) return line.host
  }
  return ''
}

function serverNameForLines(lines, managementIP) {
  const hostNode = lines.find((line) => line.node_host_id && line.name)
  if (hostNode?.name) {
    return hostNode.name.replace(/-\d+(-XHTTP)?$/i, '')
  }
  const first = lines[0]
  return first?.name || managementIP || '未命名节点服务器'
}

function aggregateNodeServers(items) {
  const groups = new Map()
  for (const node of items || []) {
    const agentHost = hostFromURL(node.agent_base_url)
    const key = node.node_host_id
      ? `host-${node.node_host_id}`
      : node.agent_base_url
        ? `agent-${node.agent_base_url}`
        : `host-${node.host || node.id}`
    if (!groups.has(key)) {
      groups.set(key, [])
    }
    groups.get(key).push({ ...node, _agent_host: agentHost })
  }

  return [...groups.entries()].map(([key, lines]) => {
    const sortedLines = [...lines].sort((a, b) => (a.sort_weight || 0) - (b.sort_weight || 0) || (a.id || 0) - (b.id || 0))
    const managementIP = managementIPForLines(sortedLines)
    const ips = collectKnownIPs(sortedLines, managementIP)
    const errors = sortedLines.filter((line) => line.last_traffic_error)
    return {
      key,
      name: serverNameForLines(sortedLines, managementIP),
      node_host_id: sortedLines.find((line) => line.node_host_id)?.node_host_id || null,
      management_ip: managementIP,
      ips,
      nodes: sortedLines,
      normal_count: sortedLines.filter((line) => line.traffic_pool !== 'residential').length,
      residential_count: sortedLines.filter((line) => line.traffic_pool === 'residential').length,
      socks5_count: sortedLines.filter((line) => line.outbound_type === 'socks5').length,
      tcp_count: sortedLines.filter((line) => (line.transport || 'tcp') === 'tcp').length,
      xhttp_count: sortedLines.filter((line) => line.transport === 'xhttp').length,
      enabled_count: sortedLines.filter((line) => line.is_enabled).length,
      last_heartbeat_at: maxDateString(sortedLines.map((line) => line.last_heartbeat_at)),
      last_traffic_report_at: maxDateString(sortedLines.map((line) => line.last_traffic_report_at)),
      last_traffic_success_at: maxDateString(sortedLines.map((line) => line.last_traffic_success_at)),
      last_traffic_error: errors[0]?.last_traffic_error || '',
      traffic_error_count: errors.reduce((sum, line) => sum + (line.traffic_error_count || 1), 0),
    }
  }).sort((a, b) => (a.management_ip || '').localeCompare(b.management_ip || '') || a.name.localeCompare(b.name))
}

function maskProxyUrl(value) {
  return String(value || '')
    .split(/\r?\n/)
    .map((line) => line.trim().replace(/^(socks5:\/\/[^:@\s]+):([^@\s]+)@/i, '$1:****@'))
    .filter(Boolean)
    .join('\n')
}

function resetForm() {
  form.name = ''
  form.host = ''
  form.traffic_pool = 'normal'
  form.outbound_type = 'direct'
  form.outbound_ip = ''
  form.outbound_proxy_url = ''
  form.transports = ['tcp']
  form.tcp_port = 443
  form.xhttp_port = 443
  form.xhttp_path = '/raypilot'
  form.xhttp_host = ''
  form.xhttp_mode = 'auto'
  form.server_name = ''
  form.public_key = ''
  form.short_id = ''
  form.line_mode = 'direct_and_relay'
  form.node_host_id = null
  form.agent_base_url = ''
  form.agent_token = ''
  form.is_enabled = true
}

function showAddDialog() {
  isEdit.value = false
  editingId.value = null
  resetForm()
  dialogVisible.value = true
}

function prefillAddDialogFromServer(server, defaults = {}) {
  isEdit.value = false
  editingId.value = null
  resetForm()
  const first = server?.nodes?.[0] || {}
  form.name = defaults.name || `${server.name}-${defaults.traffic_pool === 'residential' ? '家宽' : '普通'}`
  form.host = first.host || server.management_ip || ''
  form.traffic_pool = defaults.traffic_pool || 'normal'
  form.outbound_type = defaults.outbound_type || 'direct'
  form.outbound_ip = defaults.outbound_ip || (form.outbound_type === 'socks5' ? (server.ips?.[0] || '') : '')
  form.line_mode = defaults.line_mode || first.line_mode || 'direct_and_relay'
  form.agent_base_url = first.agent_base_url || ''
  form.server_name = first.server_name || ''
  form.public_key = first.public_key || ''
  form.short_id = first.short_id || ''
  form.node_host_id = server.node_host_id || null
  form.xhttp_path = first.xhttp_path || '/raypilot'
  form.xhttp_host = first.xhttp_host || ''
  form.xhttp_mode = first.xhttp_mode || 'auto'
  form.agent_token = ''
  activeServerKey.value = server.key
  serverDialogVisible.value = false
  dialogVisible.value = true
}

function showAddResidentialForServer(server) {
  prefillAddDialogFromServer(server, {
    traffic_pool: 'residential',
    outbound_type: 'socks5',
    line_mode: 'direct_only',
  })
}

function showAddNormalForServer(server) {
  prefillAddDialogFromServer(server, {
    traffic_pool: 'normal',
    outbound_type: 'direct',
    line_mode: 'direct_and_relay',
  })
}

function showEditDialog(row) {
  isEdit.value = true
  editingId.value = row.id
  serverDialogVisible.value = false
  form.name = row.name
  form.host = row.host
  form.traffic_pool = row.traffic_pool || 'normal'
  form.outbound_type = row.outbound_type || 'direct'
  form.outbound_ip = row.outbound_ip || ''
  form.outbound_proxy_url = row.outbound_proxy_url || ''
  form.transports = [row.transport || 'tcp']
  form.tcp_port = (row.transport || 'tcp') === 'tcp' ? row.port : 443
  form.xhttp_port = row.transport === 'xhttp' ? row.port : 443
  form.xhttp_path = row.xhttp_path || '/raypilot'
  form.xhttp_host = row.xhttp_host || ''
  form.xhttp_mode = row.xhttp_mode || 'auto'
  form.server_name = row.server_name || ''
  form.public_key = row.public_key || ''
  form.short_id = row.short_id || ''
  form.line_mode = row.line_mode || 'direct_and_relay'
  form.node_host_id = row.node_host_id || null
  form.agent_base_url = row.agent_base_url
  form.agent_token = ''
  form.is_enabled = row.is_enabled
  dialogVisible.value = true
}

async function handleSave() {
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return

  saving.value = true
  try {
    const transports = normalizedTransports(form)
    if (isEdit.value && transports.length > 1) {
      ElMessage.warning('编辑单条节点只能选择一种传输模式')
      return
    }
    if (form.outbound_type === 'socks5' && !form.outbound_proxy_url.trim()) {
      ElMessage.warning('请填写至少一条上游 SOCKS5 代理')
      return
    }
    if (transports.includes('tcp') && transports.includes('xhttp') && form.tcp_port === form.xhttp_port) {
      ElMessage.warning('TCP 和 XHTTP 端口不能相同')
      return
    }
    const primaryTransport = transports[0]
    const payload = {
      name: form.name,
      host: form.host,
      traffic_pool: form.traffic_pool,
      outbound_type: form.outbound_type,
      outbound_ip: form.outbound_ip,
      outbound_proxy_url: form.outbound_proxy_url,
      port: primaryTransport === 'xhttp' ? form.xhttp_port : form.tcp_port,
      transports,
      transport: primaryTransport,
      tcp_port: form.tcp_port,
      xhttp_port: form.xhttp_port,
      xhttp_path: form.xhttp_path,
      xhttp_host: form.xhttp_host,
      xhttp_mode: form.xhttp_mode,
      server_name: form.server_name,
      public_key: form.public_key,
      short_id: form.short_id,
      line_mode: form.line_mode,
      node_host_id: form.node_host_id,
      agent_base_url: form.agent_base_url,
      agent_token: form.agent_token,
      is_enabled: form.is_enabled,
    }

    if (isEdit.value) {
      await adminApi.nodes.update(editingId.value, payload)
      ElMessage.success('更新成功')
    } else {
      await adminApi.nodes.create(payload)
      ElMessage.success('创建成功')
    }
    dialogVisible.value = false
    await fetchNodes()
  } catch (err) {
    ElMessage.error(err.message || '操作失败')
  } finally {
    saving.value = false
  }
}

async function handleDelete(row) {
  try {
    await ElMessageBox.confirm(`确定删除节点"${row.name}"吗？`, '确认删除', { type: 'warning' })
    await adminApi.nodes.delete(row.id)
    removeNodesFromList([row.id])
    ElMessage.success('删除成功')
    fetchNodes().catch(() => {
      ElMessage.warning('删除已生效，刷新列表失败')
    })
  } catch (err) {
    if (err !== 'cancel') {
      ElMessage.error(err.message || '删除失败')
    }
  }
}

function handleServerSelectionChange(selection) {
  selectedServers.value = selection
}

function clearServerSelection() {
  serversTableRef.value?.clearSelection()
}

function showServerDialog(row) {
  activeServerKey.value = row.key
  serverDialogVisible.value = true
}

function showRepairCenterDialog(row) {
  const first = row.nodes?.[0] || {}
  repairSteps.value = []
  repairForm.ssh_host = row.management_ip || first.host || ''
  repairForm.ssh_port = 22
  repairForm.ssh_user = 'root'
  repairForm.ssh_password = ''
  repairForm.center_url = currentCenterURL()
  repairForm.center_urls_text = defaultBackupCenterURLsText()
  repairForm.node_id = row.node_host_id ? 0 : (first.id || 0)
  repairForm.node_host_id = row.node_host_id || 0
  repairForm.wait_timeout_seconds = 45
  repairDialogVisible.value = true
}

function parseCenterURLsText(text) {
  return uniqueValues(String(text || '').split(/[\s,]+/))
}

function currentCenterURL() {
  return window.location.origin
}

function defaultBackupCenterURLsText() {
  const current = currentCenterURL()
  try {
    const url = new URL(current)
    return defaultCenterFallbacks(url)
  } catch {
    return ''
  }
}

function defaultCenterFallbacks(currentURL) {
  const fallbackHostsByCurrent = {
    'leiyunai.fun': ['154.219.106.105', '154.219.106.53'],
    '154.219.106.105': ['leiyunai.fun', '154.219.106.53'],
    '154.219.106.53': ['leiyunai.fun', '154.219.106.105'],
  }
  const fallbackHosts = fallbackHostsByCurrent[currentURL.hostname] || []
  return fallbackHosts.map((host) => {
    const copy = new URL(currentURL.toString())
    copy.hostname = host
    return copy.toString().replace(/\/$/, '')
  }).join('\n')
}

async function handleRepairCenter() {
  const valid = await repairFormRef.value.validate().catch(() => false)
  if (!valid) return

  repairingCenter.value = true
  repairSteps.value = []
  try {
    const payload = {
      ssh_host: repairForm.ssh_host,
      ssh_port: repairForm.ssh_port,
      ssh_user: repairForm.ssh_user,
      ssh_password: repairForm.ssh_password,
      center_url: repairForm.center_url,
      center_urls: parseCenterURLsText(repairForm.center_urls_text),
      node_id: repairForm.node_id,
      node_host_id: repairForm.node_host_id,
      wait_timeout_seconds: repairForm.wait_timeout_seconds,
    }
    const res = await adminApi.nodes.repairCenter(payload)
    repairSteps.value = res.data.steps || []
    ElMessage.success('中心地址修复成功')
    repairDialogVisible.value = false
    await fetchNodes()
  } catch (err) {
    repairSteps.value = err?.data?.steps || repairSteps.value
    ElMessage.error(err.message || '中心地址修复失败')
  } finally {
    repairingCenter.value = false
  }
}

async function handleDeleteServer(row) {
  const lines = row.nodes || []
  if (!lines.length) return
  try {
    await ElMessageBox.confirm(`确定删除节点服务器"${row.name}"下的 ${lines.length} 条线路吗？`, '确认删除', { type: 'warning' })
  } catch {
    return
  }

  batchDeleting.value = true
  const deletedIds = []
  const failed = []
  try {
    for (const line of lines) {
      try {
        await adminApi.nodes.delete(line.id)
        deletedIds.push(line.id)
      } catch (err) {
        failed.push(`${line.name || line.id}：${err.message || '删除失败'}`)
      }
    }
    if (deletedIds.length) {
      removeNodesFromList(deletedIds)
    }
    if (failed.length) {
      ElMessage.warning(`成功删除 ${deletedIds.length} 条，失败 ${failed.length} 条：${failed.join('；')}`)
    } else {
      ElMessage.success('节点服务器删除成功')
    }
    fetchNodes().catch(() => {
      ElMessage.warning('删除已生效，刷新列表失败')
    })
  } finally {
    batchDeleting.value = false
  }
}

async function handleBatchDelete() {
  const rows = selectedServers.value.flatMap((server) => server.nodes || [])
  if (!rows.length) {
    ElMessage.warning('请选择要删除的节点服务器')
    return
  }

  try {
    await ElMessageBox.confirm(`确定批量删除选中节点服务器下的 ${rows.length} 条线路吗？`, '批量删除', { type: 'warning' })
  } catch {
    return
  }

  batchDeleting.value = true
  const deletedIds = []
  const failed = []
  try {
    for (const row of rows) {
      try {
        await adminApi.nodes.delete(row.id)
        deletedIds.push(row.id)
      } catch (err) {
        failed.push(`${row.name || row.id}：${err.message || '删除失败'}`)
      }
    }

    if (deletedIds.length) {
      removeNodesFromList(deletedIds)
    }
    if (failed.length) {
      ElMessage.warning(`成功删除 ${deletedIds.length} 个，失败 ${failed.length} 个：${failed.join('；')}`)
    } else {
      ElMessage.success('批量删除成功')
    }
    fetchNodes().catch(() => {
      ElMessage.warning('删除已生效，刷新列表失败')
    })
  } finally {
    batchDeleting.value = false
  }
}

async function handleBatchDeleteServers() {
  await handleBatchDelete()
}

function removeNodesFromList(ids) {
  const idSet = new Set(ids.map((id) => String(id)))
  nodes.value = nodes.value.filter((node) => !idSet.has(String(node.id)))
  selectedServers.value = selectedServers.value
    .map((server) => ({ ...server, nodes: (server.nodes || []).filter((node) => !idSet.has(String(node.id))) }))
    .filter((server) => server.nodes.length)
}

async function fetchNodes() {
  loading.value = true
  try {
    const res = await adminApi.nodes.list()
    nodes.value = res.data.nodes || []
  } catch (err) {
    ElMessage.error('获取节点列表失败')
  } finally {
    loading.value = false
  }
}

async function fetchNodeGroups() {
  try {
    const res = await adminApi.nodeGroups.list()
    nodeGroups.value = res.data.groups || res.data.node_groups || []
  } catch (err) {
    ElMessage.error('获取节点分组失败')
  }
}

onMounted(() => {
  fetchNodes()
  fetchNodeGroups()
})
</script>

<style scoped>
.admin-nodes {
  padding: 20px;
}
.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}
.header > div {
  display: flex;
  gap: 8px;
}
.batch-toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
  padding: 10px 12px;
  border: 1px solid #ebeef5;
  border-radius: 4px;
  background: #f5f7fa;
  color: #606266;
  font-size: 14px;
}
.deploy-steps {
  margin-top: 16px;
  padding: 12px;
  background: #f5f7fa;
  border-radius: 4px;
  max-height: 300px;
  overflow-y: auto;
}
.deploy-step {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 0;
  font-size: 14px;
}
.deploy-step .el-icon.success {
  color: #67c23a;
}
.deploy-step .el-icon.failed {
  color: #f56c6c;
}
.deploy-step .el-icon {
  color: #e6a23c;
}
.step-msg {
  margin-left: auto;
  color: #909399;
  font-size: 12px;
}
.scan-hint {
  margin-left: 10px;
  color: #909399;
  font-size: 12px;
}
.scan-ip-table {
  margin: 0 0 16px 120px;
  width: calc(100% - 120px);
}
.traffic-sync-cell {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}
.traffic-sync-cell > div {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.traffic-sync-time {
  color: #606266;
  font-size: 12px;
}
.traffic-sync-error {
  color: #f56c6c;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.line-table {
  margin: 8px 0;
}
.server-cell,
.ip-cell,
.line-outbound,
.server-dialog-summary > div:first-child {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}
.server-cell small,
.line-outbound small,
.server-dialog-summary small {
  color: #909399;
  font-size: 12px;
}
.ip-tags,
.capability-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.server-dialog {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.server-dialog-summary {
  display: grid;
  grid-template-columns: minmax(180px, 1fr) 2fr 2fr;
  gap: 12px;
  align-items: start;
}
.server-dialog-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
@media (max-width: 900px) {
  .server-dialog-summary {
    grid-template-columns: 1fr;
  }
}
</style>
