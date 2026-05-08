// Slug -> Iconify icon name (resolved against the fa7-solid collection
// in CategoryIcon.astro). Categories are a closed, developer-controlled
// set, so this mapping lives with the frontend that renders it. To
// switch icon libraries, change the prefix in CategoryIcon.astro and
// adjust any names that differ between collections.
export const ICON_BY_SLUG: Record<string, string> = {
  // Entertainment
  books: "book",
  concerts: "guitar",
  games: "gamepad",
  hobbies: "palette",
  movies: "film",
  music: "music",
  sports: "person-running",
  theater: "masks-theater",

  // Food & drink
  snacks: "cookie-bite",
  dining_out: "utensils",
  liquor: "champagne-glasses",

  // Home
  groceries: "cart-shopping",
  rent: "house",
  mortgage: "building-columns",
  electronics: "plug",
  furniture: "couch",
  household_supplies: "pump-soap",
  maintenance: "screwdriver-wrench",
  cleaning: "broom",
  pets: "paw",
  services: "bell-concierge",

  // Life
  childcare: "baby",
  clothing: "shirt",
  gym: "dumbbell",
  education: "graduation-cap",
  gifts: "gift",
  insurance: "shield-halved",
  medical: "briefcase-medical",
  taxes: "file-invoice-dollar",
  loan: "hand-holding-dollar",
  hotel: "hotel",
  legal: "scale-balanced",
  real_estate: "building-flag",

  // Transport
  bicycle: "bicycle",
  bus: "bus-side",
  car: "car-side",
  fuel: "gas-pump",
  parking: "square-parking",
  plane: "plane",
  taxi: "taxi",
  train: "train",
  special: "cable-car",

  // Utilities
  electricity: "bolt",
  heating_gas: "fire",
  internet: "wifi",
  phone: "mobile-screen",
  trash: "dumpster",
  tv: "tv",
  water: "droplet",

  // No category
  other: "meteor",
};

export const FALLBACK_ICON = "meteor";

export function iconForSlug(slug: string | undefined | null): string {
  return (slug && ICON_BY_SLUG[slug]) || FALLBACK_ICON;
}
