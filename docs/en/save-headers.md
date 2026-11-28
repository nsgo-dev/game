# Save slot headers

`Store.SaveWithHeader` writes a small JSON header beside the versioned data. `Store.Headers` lists those headers without unmarshaling the large save body. Older slots remain readable and are simply omitted from the header index until saved again.
