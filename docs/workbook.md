# Workbook contract

Changing `FINANCE_SHEET_ID` selects another workbook; it does not adapt the app to arbitrary spreadsheet layouts. Tab titles are configurable, but column positions and reporting rules remain fixed.

## Transactions tab

Row 1 must contain these exact headers in this order, in A:P:

```text
ID, Date, Month, Bank, Account, Reporting Bucket, Category, Amount (SGD), Description, Card / Source, File, Include in Expenses, Transaction Type, Transfer Link, Notes, Auto Category
```

- A — `ID`: nonempty, unique, stable string for each transaction. Blank-ID rows are skipped; duplicate IDs block writes to those entries.
- B — `Date`: text in `YYYY-MM-DD` format. Use text cells or a formula that outputs this format, rather than raw date serial numbers.
- C — `Month`: text in `YYYY-MM` format; used for period filtering and grouping.
- D — `Bank`: bank label.
- E — `Account`: account label.
- F — `Reporting Bucket`: any grouping label. Bucket controls are generated from these values.
- G — `Category`: a name from the Categories tab; editable by the app.
- H — `Amount (SGD)`: signed numeric amount in Singapore dollars. Positive values are expenses; negative included values are credits and reduce net spending. The UI displays SGD and uses the `en-SG` locale.
- I — `Description`: text; editable, limited to 500 bytes by the Go backend.
- J — `Card / Source`: optional source label.
- K — `File`: optional source filename label. This does not grant access to a local file or upload it.
- L — `Include in Expenses`: boolean or formula result indicating whether the row is included in reports.
- M — `Transaction Type`: optional type label.
- N — `Transfer Link`: optional related-transaction reference.
- O — `Notes`: optional text; editable, limited to 2000 bytes by Go.
- P — `Auto Category`: original suggested category used when restoring tracked status.

The current read/ID-resolution ranges end at row 5000 (4999 transaction rows). Rows beyond that limit are not shown or editable. Increasing the limit requires a code change and matching tests.

## Categories tab

Use A:D with a header row followed by category records:

```text
Category, Type, Description, Include in Expenses
```

Category names are in A, type labels in B, optional descriptions in C, and booleans in D. Names must be unique. The app uses column positions and skips the first row; it currently does not validate the category header names. The tab must have at least a header row. The current read range ends at row 100 (99 category records).

Include `Other` with inclusion TRUE, and `Internal Transfer` with inclusion FALSE. Add the expense categories used by transaction rows with inclusion TRUE. Other excluded categories are supported.

## Inclusion formula

Column L must follow the category's inclusion flag so a category edit also updates tracked status after refresh. For an example workbook using the default category tab title, put this formula in L2 and fill it down through your transaction rows:

```text
=IF(A2="","",IFERROR(VLOOKUP(G2,'🏷️ Categories'!A:D,4,FALSE),FALSE))
```

Adjust the tab reference if you choose another title. Google Sheets locales may require semicolons rather than commas. This formula is an example; an equivalent formula or upstream process is acceptable if it keeps L consistent with the category flags. The app never writes L itself.

The workbook must already contain its headers, category records, and transactions. The app edits existing entries; it does not create tabs, insert transactions, convert currencies, or parse statement PDFs.
