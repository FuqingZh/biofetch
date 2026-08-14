# Upstream data terms and redistribution boundary

Version: v1.0
Date: 2026-08-13
Status: current

`biofetch` is an acquisition and snapshot-locking tool. The Apache-2.0
license in the repository applies only to repository-owned source code and
documentation. It does not grant a license to any bytes downloaded from the
providers below. Users must read the current provider terms before acquiring,
using, storing, or redistributing a snapshot.

| Family | Official source / terms | Access caveat | GitHub release contents |
| --- | --- | --- | --- |
| eggNOG | [eggNOG](http://eggnog5.embl.de/) | Check current database terms and citation requirements | No upstream bytes |
| Gene Ontology | [GO downloads](https://geneontology.org/docs/download-ontology/) | Use the official release and attribution terms | No upstream bytes |
| InterPro / InterProScan | [EBI InterPro](https://www.ebi.ac.uk/interpro/download/) | InterProScan distributions and signatures may have separate terms | No upstream bytes |
| KEGG | [KEGG REST](https://www.kegg.jp/kegg/rest/keggapi.html) | Commercial or bulk use may require an appropriate KEGG subscription | No upstream bytes |
| OmniPath / DoRothEA / CollecTRI | [OmniPath](https://omnipathdb.org/) | Dataset and license query are part of snapshot identity; verify upstream terms | No upstream bytes |
| Reactome | [Reactome downloads](https://reactome.org/download-data) | Release and mapping endpoints are versioned; cite Reactome | No upstream bytes |
| STRING | [STRING downloads](https://string-db.org/cgi/download.pl) | Follow STRING data-use and citation terms | No upstream bytes |
| UniProt | [UniProt](https://www.uniprot.org/help/license) | Current UniProt license and attribution rules apply | No upstream bytes |
| WikiPathways | [WikiPathways](https://www.wikipathways.org/) | Follow pathway-specific and project terms | No upstream bytes |
| ChEBI | [ChEBI downloads](https://www.ebi.ac.uk/chebi/downloadsForward.do) | Verify current ChEBI license and attribution | No upstream bytes |
| Rhea | [Rhea](https://www.rhea-db.org/download) | Verify current Rhea license and citation | No upstream bytes |
| JASPAR | [JASPAR downloads](https://jaspar.elixir.no/downloads/) | Follow JASPAR data and citation terms | No upstream bytes |
| ChemOnt / ClassyFire | [ClassyFire](http://classyfire.wishartlab.com/) | Confirm current redistribution terms before commercial use | No upstream bytes |
| LIPID MAPS | [LIPID MAPS](https://www.lipidmaps.org/data/downloads) | Check the current LIPID MAPS terms | No upstream bytes |
| HMDB | [HMDB](https://hmdb.ca/downloads) | Browser authorization and commercial affiliation may be required; do not bypass it | No upstream bytes |
| dbCAN | [dbCAN](https://bcb.unl.edu/dbCAN2/download/) | Pinned S3 collection and source terms govern use | No upstream bytes |

The table is an engineering pointer, not legal advice. A new source proposal
must include its official terms URL, authentication requirements, release
semantics, citation, and redistribution decision before a downloader is added.
Browser or REST access is not evidence that bulk redistribution is permitted.

## Release rule

GitHub source archives and binary releases contain only `biofetch`, its
repository documentation, dependency notices, checksums, and provenance.
They must not contain database archives, CephFS paths, credentials, cookies,
browser exports, or licensed upstream payloads.
