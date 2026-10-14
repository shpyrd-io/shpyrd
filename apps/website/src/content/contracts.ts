// Content shapes consumed by the website. Editorial text lives in MDX.
export type HomeCopy = {
    "hero": {
        "heading": string;
        "description": string;
    };
    "productHunt": {
        "href": string;
        "image": string;
        "imageDark": string;
        "alt": string;
    };
    "peerPush": {
        "href": string;
        "image": string;
        "alt": string;
    };
    "gap": {
        "label": string;
        "heading": string;
        "description": string;
        "items": {
            "id": string;
            "title": string;
            "body": string;
        }[];
    };
    "route": {
        "heading": string;
        "description": string;
        "steps": {
            "id": string;
            "title": string;
            "body": string;
        }[];
    };
    "also": {
        "label": string;
        "heading": string;
        "items": {
            "href": string;
            "title": string;
            "body": string;
        }[];
    };
    "boundariesSection": {
        "title": string;
        "intro": string;
    };
    "companies": {
        "label": string;
        "heading": string;
        "description": string;
        "manifesto": {
            "href": string;
            "label": string;
        };
    };
};
export type OfferCopy = {
    signIn: { label: string; href: string };
    "discord": {
        "invite": string;
        "href": string;
    };
    "deploy": {
        "verb": string;
        "thing": string;
    }[];
    "contact": {
        "href": string;
        "label": string;
    };
    "enterpriseContact": {
        "href": string;
        "label": string;
    };
    "secondaryCta": {
        "label": string;
        "href": string;
    };
    "developerCta": {
        "label": string;
        "href": string;
    };
};
export type AgentsCopy = {
    "serverUrl": string;
    "agents": {
        "id": string;
        "name": string;
        "kind": string;
        "snippet": string;
        "file"?: string;
    }[];
    "addToAgent": {
        "label": string;
        "note": string;
        "installers": {
            "mac": {
                "name": string;
                "href": string;
            };
            "windows": {
                "name": string;
                "href": string;
            };
            "linux": {
                "name": string;
                "href": string;
            };
        };
        "manual": {
            "label": string;
            "href": string;
        };
    };
};
export type SharingCopy = {
    "intro": {
        "title": string;
        "lead": string;
    };
    "steps": {
        "id": string;
        "title": string;
        "body": string;
        "note": {
            "title": string;
            "body": string;
        };
    }[];
    "closing": {
        "title": string;
        "action": string;
    };
    "buildExample": {
        "prompt": string;
        "reply": string;
    };
};
export type NotFoundCopy = {
    "notFound": {
        "title": string;
        "description": string;
        "links": {
            "href": string;
            "label": string;
        }[];
    };
};
export type MessagesCopy = {
    "sharing": {
        "id": string;
        "headline": string;
        "explanation": string;
        "supporting": string;
    };
    "access": {
        "id": string;
        "headline": string;
        "explanation": string;
        "supporting": string;
    };
    "familiarTools": {
        "id": string;
        "headline": string;
        "explanation": string;
        "supporting": null;
    };
};
export type BoundariesCopy = {
    "boundaries": {
        "id": string;
        "claim": string;
        "limit": string;
        "step": string | null;
    }[];
};
export type PricingCopy = {
    "pricing": {
        "title": string;
        "hero": {
            "heading": string;
            "description": string;
        };
        "plans": {
            "id": string;
            "name": string;
            "per": string;
            "summary": string;
            "features": (string)[];
            "action": {
                "label": string;
                "kind": string;
            };
            "footnote": string;
        }[];
        "enterprise": {
            "name": string;
            "summary": string;
            "features": (string)[];
            "action": {
                "label": string;
                "kind": string;
            };
        };
        "usage": {
            "id": string;
            "name": string;
            "note": string;
        }[];
        "counts": {
            "heading": string;
            "items": {
                "id": string;
                "title": string;
                "body": string;
            }[];
        };
        "example": {
            "heading": string;
            "description": string;
            "lines": {
                "what": string;
                "cpuCores"?: number;
                "memoryGib"?: number;
                "hours"?: number;
                "storageGib"?: number;
                "egressGib"?: number;
            }[];
            "sense": string;
        };
        "faq": {
            "q": string;
            "a": string;
        }[];
        "footnotes": {
            "international": {
                "text": string;
                "other": {
                    "label": string;
                    "href": string;
                };
            };
            "br": {
                "text": string;
                "other": {
                    "label": string;
                    "href": string;
                };
            };
        };
        "included": string;
        "earlyInvoice": string;
    };
};
export type SalesContactCopy = {
    "page": {
        "title": string;
        "description": string;
        "submit": string;
        "beside": {
            "heading": string;
            "items": (string)[];
        };
        "after": {
            "heading": string;
            "items": (string)[];
        };
    };
};
export type EnterpriseContactCopy = {
    "page": {
        "title": string;
        "description": string;
        "submit": string;
        "beside": {
            "heading": string;
        };
        "after": {
            "heading": string;
            "items": (string)[];
        };
    };
};
export type ContactFormCopy = {
    "fields": {
        "name": string;
        "label": string;
    }[];
    "companySizes": {
        "value": string;
        "label": string;
    }[];
    "privacy": {
        "label": string;
        "href": string;
    };
    "technical": {
        "text": string;
        "link": string;
    };
    "failed": string;
    "thanks": string;
    "replyFirst": string;
    "errors": {
        "text": string;
        "required": string;
        "length": string;
        "email": string;
        "choice": string;
    };
};
export type SolutionGroupsCopy = {
    "groups": {
        "id": string;
        "name": string;
        "order": number;
    }[];
};
