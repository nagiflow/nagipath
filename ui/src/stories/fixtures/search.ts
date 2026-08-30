import { SearchResponseSchema } from '../../api/pb/nagipath/api/v1/search_pb'
import { pb } from '../support/mockApi'

export const searchFixture = pb(SearchResponseSchema, {
  query: 'proxy_pass',
  matchMode: 'substring',
  scope: 'rules',
  matches: 1842,
  filesHit: 214,
  nodesHit: 312,
  instances: 402,
  from: 1,
  to: 2,
  page: 1,
  pages: 107,
  duration: '38 ms',
  vendors: [{ value: 'nginx', count: 1720 }, { value: 'haproxy', count: 96 }, { value: 'apache', count: 26 }],
  files: [{ value: '/etc/nginx/conf.d/checkout.conf', count: 312 }, { value: '/etc/nginx/nginx.conf', count: 312 }],
  clusters: [{ value: 'app-iad3', count: 1204 }, { value: 'app-sfo2', count: 512 }, { value: 'legacy', count: 126 }],
  snapshotAge: [{ value: '< 6h', count: 1740 }, { value: '6-24h', count: 76 }, { value: '> 24h', count: 26 }],
  selVendor: ['nginx'],
  groups: [
    {
      instanceId: 900n,
      instance: 'nginx (:443)',
      node: 'app-iad3-17',
      vendor: 'nginx',
      snapshotId: 8801n,
      fileId: 9102n,
      path: '/etc/nginx/conf.d/checkout.conf',
      rules: [
        { ruleId: 4002n, instanceId: 900n, snapshotId: 8801n, fileId: 9102n, instance: 'nginx (:443)', vendor: 'nginx', node: 'app-iad3-17', cluster: 'app-iad3', directive: 'proxy_pass', actionClass: 'proxy', args: 'http://checkout_api_canary', raw: 'proxy_pass http://checkout_api_canary;', path: '/etc/nginx/conf.d/checkout.conf', byteStart: 384 },
        { ruleId: 4004n, instanceId: 900n, snapshotId: 8801n, fileId: 9102n, instance: 'nginx (:443)', vendor: 'nginx', node: 'app-iad3-17', cluster: 'app-iad3', directive: 'proxy_pass', actionClass: 'proxy', args: 'http://checkout_app', raw: 'proxy_pass http://checkout_app;', path: '/etc/nginx/conf.d/checkout.conf', byteStart: 704, shadowed: true },
      ],
      texts: [
        { fileId: 9102n, snapshotId: 8801n, instanceId: 900n, instance: 'nginx (:443)', vendor: 'nginx', node: 'app-iad3-17', cluster: 'app-iad3', path: '/etc/nginx/conf.d/checkout.conf', snippet: '        proxy_pass http://checkout_api_canary;' },
      ],
    },
    {
      instanceId: 640n,
      instance: 'haproxy (:443)',
      node: 'app-sfo2-04',
      vendor: 'haproxy',
      snapshotId: 8712n,
      fileId: 8802n,
      path: '/etc/haproxy/haproxy.cfg',
      texts: [
        { fileId: 8802n, snapshotId: 8712n, instanceId: 640n, instance: 'haproxy (:443)', vendor: 'haproxy', node: 'app-sfo2-04', cluster: 'app-sfo2', path: '/etc/haproxy/haproxy.cfg', snippet: '    server app1 10.9.3.11:8080 check # was proxy_pass in the nginx era' },
      ],
    },
  ],
})

export const unaskedSearchFixture = pb(SearchResponseSchema, { matchMode: 'substring', scope: 'rules' })

export const regexErrorSearchFixture = pb(SearchResponseSchema, {
  query: 'proxy_pass(',
  matchMode: 'regex',
  scope: 'rules',
  regexError: 'error parsing regexp: missing closing ): `proxy_pass(`',
})
