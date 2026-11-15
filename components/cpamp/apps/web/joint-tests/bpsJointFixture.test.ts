/// <reference types="node" />
import { readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { expect,it,vi } from 'vitest';
import { apiClient } from '@/services/api/client';
import { credentialConcurrencyApi } from '@/services/api/credentialConcurrency';
import { cpaBpsQuotaApi } from '@/services/api/cpaBpsQuota';
import { cpaBpsResetCount } from '@/features/accounts/model/cpaBpsQuotaPresentation';
vi.mock('@/services/api/client',async(original)=>({...await original<typeof import('@/services/api/client')>(),apiClient:{get:vi.fn()}}));
vi.mock('@/services/api/credentialConcurrency',()=>({credentialConcurrencyApi:{list:vi.fn()}}));
const dir=process.env.BPS_JOINT_DIR;
it.skipIf(!dir).each(['missing','null','zero'])('联合共享实际RPC→管理GET→Manager输出→Web：%s',async name=>{
 const raw=JSON.parse(readFileSync(join(dir!,name+'.manager-output.json'),'utf8'));
 vi.mocked(credentialConcurrencyApi.list).mockResolvedValue({mode:'local',connection_ref:'joint',default_limit:20,pool_waiting:0,credentials:[{provider:'cpa-bps',auth_id:'synthetic-auth',auth_index:'synthetic-index',source:'runtime',editable:false,name:'fixture',read_only_reason:'plugin_virtual',concurrency_limit:null,effective_limit:20,limit_source:'default',active:0,pinned_waiting:0}]});
 vi.mocked(apiClient.get).mockResolvedValue(raw);
 const result=await cpaBpsQuotaApi.query('synthetic-index',{apiBase:'http://joint.invalid',managementKey:'fixture'},'synthetic-auth');
 const count=cpaBpsResetCount(result.quota);
 expect(count).toBe(name==='zero'?0:null);
 writeFileSync(join(dir!,name+'.web-output.json'),JSON.stringify({provider:result.provider,auth_id:result.auth_id,auth_index:result.auth_index,count,status:count===null?'unknown':'known',source:result.source,query_mode:'plugin_fetch',quota:result.quota},null,2));
});
