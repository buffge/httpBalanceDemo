# http负载均衡服务 demo

服务分为 主服务,监控,上游服务器三个模块

主服务为用户提供服务,请求到来时选择一个上游服务器转发请求

监控服务器每隔一段时间请求一次上游服务器,如果失败则摘掉,如果请求成功则恢复

轮询策略使用一致性哈希实现
