# 存档槽位摘要

`Store.SaveWithHeader` 在版本化数据旁写入小型 JSON 摘要。`Store.Headers` 无需反序列化大型存档正文即可列出摘要。旧槽位仍可读取，在再次保存前不会出现在摘要索引中。
